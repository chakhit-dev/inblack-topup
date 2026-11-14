package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/inblack/topup-backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const FulfillmentStreamKey = "stream:fulfillment"

type WebhookHandler struct {
	db            *pgxpool.Pool
	redis         *redis.Client
	webhookSecret string
}

func NewWebhookHandler(db *pgxpool.Pool, rdb *redis.Client, secret string) *WebhookHandler {
	return &WebhookHandler{
		db:            db,
		redis:         rdb,
		webhookSecret: secret,
	}
}

type PaymentWebhookPayload struct {
	EventID    string    `json:"event_id" binding:"required"`
	EventType  string    `json:"event_type" binding:"required"` // payment.succeeded, payment.failed
	OrderID    string    `json:"order_id" binding:"required"`
	Amount     int64     `json:"amount" binding:"required"`
	OccurredAt time.Time `json:"occurred_at" binding:"required"`
}

// HandlePaymentWebhook POST /api/v1/webhooks/payment
func (h *WebhookHandler) HandlePaymentWebhook(c *gin.Context) {
	ctx := c.Request.Context()

	// อ่าน Raw Body เพื่อตรวจสอบ HMAC-SHA256 Signature
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, domain.NewProblem(
			http.StatusBadRequest, domain.CodeValidationFailed, "Invalid Body", "Cannot read webhook payload",
		))
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(rawBody))

	sigHeader := strings.TrimSpace(c.GetHeader("X-Webhook-Signature"))
	sigHeader = strings.TrimPrefix(sigHeader, "sha256=")
	if !h.verifySignature(rawBody, sigHeader) {
		c.JSON(http.StatusUnauthorized, domain.NewProblem(
			http.StatusUnauthorized, "INVALID_SIGNATURE", "Invalid Webhook Signature", "HMAC signature verification failed",
		))
		return
	}

	var payload PaymentWebhookPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, domain.NewProblem(
			http.StatusBadRequest, domain.CodeValidationFailed, "Invalid Webhook Payload", err.Error(),
		))
		return
	}

	if err := domain.ValidateOrderID(payload.OrderID); err != nil {
		c.JSON(http.StatusBadRequest, domain.NewProblem(
			http.StatusBadRequest, domain.CodeValidationFailed, "Invalid Order ID", "Order ID format or checksum is invalid",
		))
		return
	}

	// เริ่ม Database Transaction
	tx, err := h.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Transaction Error", "Failed to start transaction",
		))
		return
	}
	defer tx.Rollback(ctx)

	// เช็ก Idempotency ของ Webhook ด้วย event_id
	var existingStatus string
	err = tx.QueryRow(ctx, `SELECT status FROM webhook_events WHERE event_id = $1 FOR UPDATE`, payload.EventID).Scan(&existingStatus)
	if err == nil {
		// เคยประมวลผล event_id นี้ไปแล้ว ตอบ 200 OK ทันที (Idempotent)
		c.JSON(http.StatusOK, gin.H{"status": "already_processed", "event_id": payload.EventID})
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to check webhook event",
		))
		return
	}

	// ล็อกแถว Order เพื่ออัปเดตสถานะอย่างปลอดภัย
	var currentStatus string
	var orderTotal int64
	err = tx.QueryRow(ctx, `
		SELECT status, total
		FROM orders
		WHERE id = $1
		FOR UPDATE
	`, payload.OrderID).Scan(&currentStatus, &orderTotal)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, domain.NewProblem(
				http.StatusNotFound, domain.CodeNotFound, "Order Not Found", "Order ID does not exist",
			))
			return
		}
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to lock order",
		))
		return
	}

	// บันทึกประวัติ Webhook ลงตาราง webhook_events
	rawJSON, _ := json.Marshal(payload)
	_, err = tx.Exec(ctx, `
		INSERT INTO webhook_events (event_id, order_id, event_type, amount, raw_payload, status, occurred_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, 'PROCESSED', $6)
	`, payload.EventID, payload.OrderID, payload.EventType, payload.Amount, string(rawJSON), payload.OccurredAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to record webhook event",
		))
		return
	}

	status := domain.OrderStatus(currentStatus)
	enqueueFulfillment := false

	switch payload.EventType {
	case "payment.succeeded":
		// กรณีออเดอร์ถูกจ่ายเงินไปแล้ว (เช่น Gateway ส่งคนละ event_id มาซ้ำ)
		if status == domain.StatusPaid || status == domain.StatusInQueue || status == domain.StatusInProgress || status == domain.StatusCompleted {
			if err := tx.Commit(ctx); err != nil {
				c.JSON(http.StatusInternalServerError, domain.NewProblem(http.StatusInternalServerError, domain.CodeInternalServerError, "Commit Error", err.Error()))
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": "ignored_duplicate_payment", "order_status": currentStatus})
			return
		}

		// ตรวจสอบยอดเงินตรงกับใบแจ้งหนี้หรือไม่
		if payload.Amount != orderTotal {
			reason := fmt.Sprintf("Payment amount mismatch: expected %d, got %d", orderTotal, payload.Amount)
			if err := transitionOrder(ctx, tx, payload.OrderID, status, domain.StatusNeedsReview, "webhook", reason); err != nil {
				c.JSON(http.StatusInternalServerError, domain.NewProblem(http.StatusInternalServerError, domain.CodeInternalServerError, "Transition Error", err.Error()))
				return
			}
			break
		}

		// กรณีปกติ (PENDING_PAYMENT) หรือจ่ายช้า (EXPIRED)
		if status == domain.StatusPendingPayment || status == domain.StatusExpired {
			// ถ้าเคย EXPIRED ไปแล้ว ต้องตรวจสอบว่ายังสามารถดึงสต็อก/สิทธิ์คืนได้ไหม
			if status == domain.StatusExpired {
				canRecover, recoverErr := h.reclaimExpiredResources(ctx, tx, payload.OrderID)
				if recoverErr != nil {
					c.JSON(http.StatusInternalServerError, domain.NewProblem(http.StatusInternalServerError, domain.CodeInternalServerError, "Recovery Error", recoverErr.Error()))
					return
				}
				if !canRecover {
					reason := "Late payment received after expiration, but stock or voucher quota is no longer available"
					if err := transitionOrder(ctx, tx, payload.OrderID, status, domain.StatusNeedsReview, "webhook", reason); err != nil {
						c.JSON(http.StatusInternalServerError, domain.NewProblem(http.StatusInternalServerError, domain.CodeInternalServerError, "Transition Error", err.Error()))
						return
					}
					break
				}
			}

			// เปลี่ยนสถานะเป็น PAID -> IN_QUEUE
			if err := transitionOrder(ctx, tx, payload.OrderID, status, domain.StatusPaid, "webhook", "Payment confirmed via gateway webhook"); err != nil {
				c.JSON(http.StatusInternalServerError, domain.NewProblem(http.StatusInternalServerError, domain.CodeInternalServerError, "Transition Error", err.Error()))
				return
			}
			if err := transitionOrder(ctx, tx, payload.OrderID, domain.StatusPaid, domain.StatusInQueue, "system", "Enqueued for automated fulfillment"); err != nil {
				c.JSON(http.StatusInternalServerError, domain.NewProblem(http.StatusInternalServerError, domain.CodeInternalServerError, "Transition Error", err.Error()))
				return
			}
			enqueueFulfillment = true
		} else {
			reason := fmt.Sprintf("Unexpected payment.succeeded when order is in status %s", status)
			if err := transitionOrder(ctx, tx, payload.OrderID, status, domain.StatusNeedsReview, "webhook", reason); err != nil {
				c.JSON(http.StatusInternalServerError, domain.NewProblem(http.StatusInternalServerError, domain.CodeInternalServerError, "Transition Error", err.Error()))
				return
			}
		}

	case "payment.failed":
		if status == domain.StatusPendingPayment {
			if err := transitionOrder(ctx, tx, payload.OrderID, status, domain.StatusPaymentFailed, "webhook", "Payment failed at gateway"); err != nil {
				c.JSON(http.StatusInternalServerError, domain.NewProblem(http.StatusInternalServerError, domain.CodeInternalServerError, "Transition Error", err.Error()))
				return
			}
			if err := releaseOrderResources(ctx, tx, payload.OrderID); err != nil {
				c.JSON(http.StatusInternalServerError, domain.NewProblem(http.StatusInternalServerError, domain.CodeInternalServerError, "Resource Release Error", err.Error()))
				return
			}
		}

	default:
		c.JSON(http.StatusBadRequest, domain.NewProblem(
			http.StatusBadRequest, domain.CodeValidationFailed, "Unsupported Event Type", "Unsupported webhook event_type",
		))
		return
	}

	if err := tx.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Commit Error", "Failed to commit webhook transaction",
		))
		return
	}

	// ส่งงานเข้า Redis Stream หลัง Commit สำเร็จ
	if enqueueFulfillment {
		_ = h.redis.XAdd(ctx, &redis.XAddArgs{
			Stream: FulfillmentStreamKey,
			Values: map[string]interface{}{
				"order_id":    payload.OrderID,
				"enqueued_at": time.Now().UTC().Format(time.RFC3339),
			},
		}).Err()
	}

	c.JSON(http.StatusOK, gin.H{
		"status":   "processed",
		"event_id": payload.EventID,
		"order_id": payload.OrderID,
	})
}

func (h *WebhookHandler) verifySignature(payload []byte, signatureHex string) bool {
	if h.webhookSecret == "" || signatureHex == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(h.webhookSecret))
	mac.Write(payload)
	expected := mac.Sum(nil)

	provided, err := hex.DecodeString(signatureHex)
	if err != nil {
		return false
	}
	return hmac.Equal(expected, provided)
}

func transitionOrder(ctx context.Context, tx pgx.Tx, orderID string, from, to domain.OrderStatus, actor, reason string) error {
	if !domain.CanTransition(from, to) {
		return fmt.Errorf("invalid transition from %s to %s", from, to)
	}
	if _, err := tx.Exec(ctx, `UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2`, string(to), orderID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO order_events (order_id, from_status, to_status, actor, reason)
		VALUES ($1, $2, $3, $4, $5)
	`, orderID, string(from), string(to), actor, reason)
	return err
}

// releaseOrderResources คืนสต็อกสินค้าและสิทธิ์ Voucher เมื่อออเดอร์หมดอายุหรือจ่ายเงินล้มเหลว
func releaseOrderResources(ctx context.Context, tx pgx.Tx, orderID string) error {
	// คืนสต็อกสินค้า (เฉพาะสินค้าที่มี stock จำกัด)
	_, err := tx.Exec(ctx, `
		UPDATE products p
		SET stock = p.stock + oi.quantity, updated_at = NOW()
		FROM order_items oi
		WHERE oi.order_id = $1 AND oi.product_id = p.id AND p.stock IS NOT NULL
	`, orderID)
	if err != nil {
		return err
	}

	// ปิดการใช้งาน voucher_usages และลด used_count
	var voucherID string
	err = tx.QueryRow(ctx, `
		UPDATE voucher_usages
		SET is_active = FALSE
		WHERE order_id = $1 AND is_active = TRUE
		RETURNING voucher_id
	`, orderID).Scan(&voucherID)
	if err == nil && voucherID != "" {
		_, err = tx.Exec(ctx, `
			UPDATE vouchers
			SET used_count = GREATEST(used_count - 1, 0), updated_at = NOW()
			WHERE id = $1
		`, voucherID)
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

// reclaimExpiredResources พยายามหักสต็อกและดึงสิทธิ์ Voucher คืนกรณียอดเงินเข้าช้าหลัง EXPIRED
func (h *WebhookHandler) reclaimExpiredResources(ctx context.Context, tx pgx.Tx, orderID string) (bool, error) {
	// 1. ตรวจสอบและหักสต็อกสินค้าอีกครั้ง
	rows, err := tx.Query(ctx, `
		SELECT oi.product_id, oi.quantity, p.stock
		FROM order_items oi
		JOIN products p ON p.id = oi.product_id
		WHERE oi.order_id = $1
		FOR UPDATE OF p
	`, orderID)
	if err != nil {
		return false, err
	}
	type itemStock struct {
		productID string
		qty       int
		stock     *int
	}
	var items []itemStock
	for rows.Next() {
		var it itemStock
		if err := rows.Scan(&it.productID, &it.qty, &it.stock); err != nil {
			rows.Close()
			return false, err
		}
		items = append(items, it)
	}
	rows.Close()

	for _, it := range items {
		if it.stock != nil {
			if *it.stock < it.qty {
				return false, nil // สต็อกหมดแล้วระหว่างที่รอจ่ายเงิน
			}
			if _, err := tx.Exec(ctx, `UPDATE products SET stock = stock - $1, updated_at = NOW() WHERE id = $2`, it.qty, it.productID); err != nil {
				return false, err
			}
		}
	}

	// ตรวจสอบและดึงสิทธิ์ Voucher กลับคืน (ถ้าออเดอร์นี้เคยใช้โค้ดส่วนลด)
	var voucherID string
	err = tx.QueryRow(ctx, `SELECT voucher_id FROM voucher_usages WHERE order_id = $1`, orderID).Scan(&voucherID)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil // ไม่ได้ใช้ voucher
	}
	if err != nil {
		return false, err
	}

	var usageLimit *int
	var usedCount int
	var isActive bool
	err = tx.QueryRow(ctx, `
		SELECT usage_limit, used_count, is_active
		FROM vouchers WHERE id = $1 FOR UPDATE
	`, voucherID).Scan(&usageLimit, &usedCount, &isActive)
	if err != nil {
		return false, err
	}

	if !isActive || (usageLimit != nil && usedCount >= *usageLimit) {
		return false, nil // โควตา voucher เต็มไปแล้ว
	}

	if _, err := tx.Exec(ctx, `UPDATE vouchers SET used_count = used_count + 1, updated_at = NOW() WHERE id = $1`, voucherID); err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `UPDATE voucher_usages SET is_active = TRUE WHERE order_id = $1`, orderID)
	return err == nil, err
}
