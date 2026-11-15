package handler

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/inblack/topup-backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type AdminHandler struct {
	db    *pgxpool.Pool
	redis *redis.Client
}

func NewAdminHandler(db *pgxpool.Pool, rdb *redis.Client) *AdminHandler {
	return &AdminHandler{
		db:    db,
		redis: rdb,
	}
}

// ListOrders GET /api/v1/admin/orders
func (h *AdminHandler) ListOrders(c *gin.Context) {
	ctx := c.Request.Context()

	statusFilter := strings.ToUpper(strings.TrimSpace(c.Query("status")))
	emailFilter := strings.ToLower(strings.TrimSpace(c.Query("email")))
	orderIDFilter := strings.TrimSpace(c.Query("order_id"))
	cursorStr := strings.TrimSpace(c.Query("cursor"))

	limit := 20
	if lStr := c.Query("limit"); lStr != "" {
		if parsed, err := strconv.Atoi(lStr); err == nil && parsed > 0 {
			if parsed > 100 {
				parsed = 100
			}
			limit = parsed
		}
	}

	var conditions []string
	var args []interface{}
	argIdx := 1

	if statusFilter != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, statusFilter)
		argIdx++
	}
	if emailFilter != "" {
		conditions = append(conditions, fmt.Sprintf("LOWER(customer_email) = $%d", argIdx))
		args = append(args, emailFilter)
		argIdx++
	}
	if orderIDFilter != "" {
		conditions = append(conditions, fmt.Sprintf("id = $%d", argIdx))
		args = append(args, orderIDFilter)
		argIdx++
	}

	if cursorStr != "" {
		cTime, cID, err := decodeOrderCursor(cursorStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, domain.NewProblem(
				http.StatusBadRequest, domain.CodeValidationFailed, "Invalid Cursor", "Malformed pagination cursor",
			))
			return
		}
		conditions = append(conditions, fmt.Sprintf("(created_at, id) < ($%d, $%d)", argIdx, argIdx+1))
		args = append(args, cTime, cID)
		argIdx += 2
	}

	query := `
		SELECT id, customer_email, game_uid, payment_method, voucher_code,
		       subtotal, discount, net, fee, total, vat, status, expires_at, created_at
		FROM orders
	`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", argIdx)
	args = append(args, limit+1)

	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to query orders",
		))
		return
	}
	defer rows.Close()

	orders := make([]OrderResp, 0)
	for rows.Next() {
		var o OrderResp
		if err := rows.Scan(
			&o.ID, &o.CustomerEmail, &o.GameUID, &o.PaymentMethod, &o.VoucherCode,
			&o.Subtotal, &o.Discount, &o.Net, &o.Fee, &o.Total, &o.VAT,
			&o.Status, &o.ExpiresAt, &o.CreatedAt,
		); err != nil {
			c.JSON(http.StatusInternalServerError, domain.NewProblem(
				http.StatusInternalServerError, domain.CodeInternalServerError, "Scan Error", "Failed to scan order row",
			))
			return
		}
		o.Items = []OrderItemResp{}
		orders = append(orders, o)
	}

	var nextCursor *string
	if len(orders) > limit {
		orders = orders[:limit]
		last := orders[len(orders)-1]
		encoded := encodeOrderCursor(last.CreatedAt, last.ID)
		nextCursor = &encoded
	}

	c.JSON(http.StatusOK, gin.H{
		"data": orders,
		"pagination": gin.H{
			"next_cursor": nextCursor,
			"limit":       limit,
		},
	})
}

type RefundOrderReq struct {
	Amount *int64 `json:"amount"` // หากไม่ระบุ จะคืนเงินเต็มจำนวนที่เหลืออยู่
	Reason string `json:"reason" binding:"required"`
}

// RefundOrder POST /api/v1/admin/orders/:id/refund
func (h *AdminHandler) RefundOrder(c *gin.Context) {
	ctx := c.Request.Context()
	orderID := strings.TrimSpace(c.Param("id"))
	adminEmail := c.GetString("user_email")

	if err := domain.ValidateOrderID(orderID); err != nil {
		c.JSON(http.StatusBadRequest, domain.NewProblem(
			http.StatusBadRequest, domain.CodeValidationFailed, "Invalid Order ID", "Order ID format or checksum is invalid",
		))
		return
	}

	idemKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idemKey == "" {
		c.JSON(http.StatusBadRequest, domain.NewProblem(
			http.StatusBadRequest, domain.CodeValidationFailed, "Missing Idempotency-Key", "Idempotency-Key header is required for refunds",
		))
		return
	}

	var req RefundOrderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, domain.NewProblem(
			http.StatusBadRequest, domain.CodeValidationFailed, "Validation Failed", err.Error(),
		))
		return
	}

	rawHashBytes := sha256.Sum256([]byte(fmt.Sprintf("refund|%s|%v|%s", orderID, req.Amount, strings.TrimSpace(req.Reason))))
	reqHash := hex.EncodeToString(rawHashBytes[:])

	tx, err := h.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Transaction Error", "Failed to start transaction",
		))
		return
	}
	defer tx.Rollback(ctx)

	_, _ = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", "refund:"+idemKey)

	var existingHash string
	var existingStatus int
	var existingBody []byte
	err = tx.QueryRow(ctx, `
		SELECT request_hash, response_status, response_body
		FROM idempotency_keys
		WHERE idempotency_key = $1
	`, idemKey).Scan(&existingHash, &existingStatus, &existingBody)
	if err == nil {
		if existingHash != reqHash {
			c.JSON(http.StatusUnprocessableEntity, domain.NewProblem(
				http.StatusUnprocessableEntity, domain.CodeIdempotencyKeyReused, "Idempotency Key Reused", "Idempotency-Key was used with a different refund request",
			))
			return
		}
		c.Data(existingStatus, "application/json; charset=utf-8", existingBody)
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to verify idempotency key",
		))
		return
	}

	// ล็อกออเดอร์เพื่อตรวจสอบสถานะและยอดเงิน
	var currentStatus string
	var orderTotal int64
	err = tx.QueryRow(ctx, `SELECT status, total FROM orders WHERE id = $1 FOR UPDATE`, orderID).Scan(&currentStatus, &orderTotal)
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

	status := domain.OrderStatus(currentStatus)
	if status != domain.StatusCompleted && status != domain.StatusFailed && status != domain.StatusNeedsReview {
		c.JSON(http.StatusConflict, domain.NewProblem(
			http.StatusConflict, domain.CodeInvalidTransition, "Invalid Order Status for Refund",
			fmt.Sprintf("Cannot refund order in status %s", status),
		))
		return
	}

	// คำนวณยอดที่เคยคืนเงินไปแล้ว
	var alreadyRefunded int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount), 0) FROM refunds WHERE order_id = $1 AND status = 'SUCCEEDED'
	`, orderID).Scan(&alreadyRefunded); err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to check prior refunds",
		))
		return
	}

	remaining := orderTotal - alreadyRefunded
	if remaining <= 0 {
		c.JSON(http.StatusConflict, domain.NewProblem(
			http.StatusConflict, domain.CodeInvalidTransition, "Already Fully Refunded", "Order has already been fully refunded",
		))
		return
	}

	refundAmount := remaining
	if req.Amount != nil {
		if *req.Amount <= 0 || *req.Amount > remaining {
			c.JSON(http.StatusUnprocessableEntity, domain.NewProblem(
				http.StatusUnprocessableEntity, domain.CodeValidationFailed, "Invalid Refund Amount",
				fmt.Sprintf("Refund amount must be between 1 and %d satang", remaining),
			))
			return
		}
		refundAmount = *req.Amount
	}

	var refundID string
	var createdAt time.Time
	err = tx.QueryRow(ctx, `
        INSERT INTO refunds (order_id, idempotency_key, amount, reason, status, refunded_by)
        VALUES ($1, $2, $3, $4, 'SUCCEEDED', $5)
        RETURNING id, created_at
    `, orderID, idemKey, refundAmount, strings.TrimSpace(req.Reason), adminEmail).Scan(&refundID, &createdAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to insert refund record",
		))
		return
	}

	newTotalRefunded := alreadyRefunded + refundAmount
	if _, err := tx.Exec(ctx, `UPDATE orders SET refunded_amount = $1, updated_at = NOW() WHERE id = $2`, newTotalRefunded, orderID); err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to update order refunded amount",
		))
		return
	}
	newStatus := string(status)
	if newTotalRefunded >= orderTotal {
		if err := transitionOrder(ctx, tx, orderID, status, domain.StatusRefunded, adminEmail, "Full refund processed: "+strings.TrimSpace(req.Reason)); err != nil {
			c.JSON(http.StatusInternalServerError, domain.NewProblem(
				http.StatusInternalServerError, domain.CodeInternalServerError, "Transition Error", err.Error(),
			))
			return
		}

		newStatus = string(domain.StatusRefunded)
		// คืนสิทธิ์ Voucher หากคืนเงินเต็มจำนวน
		_ = releaseOrderResources(ctx, tx, orderID)
	} else {
		_, _ = tx.Exec(ctx, `
			INSERT INTO order_events (order_id, from_status, to_status, actor, reason)
			VALUES ($1, $2, $2, $3, $4)
		`, orderID, string(status), adminEmail, fmt.Sprintf("Partial refund of %d satang: %s", refundAmount, strings.TrimSpace(req.Reason)))
	}

	respPayload := gin.H{
		"data": gin.H{
			"refund_id":       refundID,
			"order_id":        orderID,
			"refunded_amount": refundAmount,
			"total_refunded":  newTotalRefunded,
			"order_total":     orderTotal,
			"order_status":    newStatus,
			"initiated_by":    adminEmail,
			"reason":          strings.TrimSpace(req.Reason),
			"created_at":      createdAt,
		},
	}

	respBytes, _ := json.Marshal(respPayload)
	if _, err := tx.Exec(ctx, `
		INSERT INTO idempotency_keys (idempotency_key, request_hash, response_status, response_body)
		VALUES ($1, $2, $3, $4::jsonb)
	`, idemKey, reqHash, http.StatusOK, string(respBytes)); err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to save idempotency key",
		))
		return
	}

	if err := tx.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Commit Error", "Failed to commit refund",
		))
		return
	}

	c.Data(http.StatusOK, "application/json; charset=utf-8", respBytes)
}

// RetryFulfillment POST /api/v1/admin/orders/:id/retry-fulfillment
func (h *AdminHandler) RetryFulfillment(c *gin.Context) {
	ctx := c.Request.Context()
	orderID := strings.TrimSpace(c.Param("id"))
	adminEmail := c.GetString("user_email")

	if err := domain.ValidateOrderID(orderID); err != nil {
		c.JSON(http.StatusBadRequest, domain.NewProblem(
			http.StatusBadRequest, domain.CodeValidationFailed, "Invalid Order ID", "Order ID format or checksum is invalid",
		))
		return
	}

	tx, err := h.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Transaction Error", "Failed to start transaction",
		))
		return
	}
	defer tx.Rollback(ctx)

	var currentStatus string
	err = tx.QueryRow(ctx, `SELECT status FROM orders WHERE id = $1 FOR UPDATE`, orderID).Scan(&currentStatus)
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

	status := domain.OrderStatus(currentStatus)
	if status != domain.StatusFailed {
		c.JSON(http.StatusConflict, domain.NewProblem(
			http.StatusConflict, domain.CodeInvalidTransition, "Invalid Status for Retry",
			fmt.Sprintf("Only FAILED orders can be retried (current status: %s)", status),
		))
		return
	}

	if err := transitionOrder(ctx, tx, orderID, domain.StatusFailed, domain.StatusInQueue, adminEmail, "Admin manually triggered fulfillment retry"); err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Transition Error", err.Error(),
		))
		return
	}

	if err := tx.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Commit Error", "Failed to commit retry",
		))
		return
	}

	// ส่งกลับเข้า Redis Stream เพื่อให้ Worker หยิบไปเติมเกมทันที
	_ = h.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: FulfillmentStreamKey,
		Values: map[string]interface{}{
			"order_id":    orderID,
			"retried_by":  adminEmail,
			"enqueued_at": time.Now().UTC().Format(time.RFC3339),
		},
	}).Err()

	c.JSON(http.StatusOK, gin.H{
		"message":  "Order re-queued for fulfillment",
		"order_id": orderID,
		"status":   string(domain.StatusInQueue),
	})
}

// ListAuditLogs GET /api/v1/admin/audit-logs
func (h *AdminHandler) ListAuditLogs(c *gin.Context) {
	ctx := c.Request.Context()

	orderIDFilter := strings.TrimSpace(c.Query("order_id"))
	actorFilter := strings.TrimSpace(c.Query("actor"))
	cursorStr := strings.TrimSpace(c.Query("cursor"))

	limit := 50
	if lStr := c.Query("limit"); lStr != "" {
		if parsed, err := strconv.Atoi(lStr); err == nil && parsed > 0 {
			if parsed > 200 {
				parsed = 200
			}
			limit = parsed
		}
	}

	var conditions []string
	var args []interface{}
	argIdx := 1

	if orderIDFilter != "" {
		conditions = append(conditions, fmt.Sprintf("order_id = $%d", argIdx))
		args = append(args, orderIDFilter)
		argIdx++
	}
	if actorFilter != "" {
		conditions = append(conditions, fmt.Sprintf("actor = $%d", argIdx))
		args = append(args, actorFilter)
		argIdx++
	}
	if cursorStr != "" {
		if lastID, err := strconv.ParseInt(cursorStr, 10, 64); err == nil {
			conditions = append(conditions, fmt.Sprintf("id < $%d", argIdx))
			args = append(args, lastID)
			argIdx++
		}
	}

	query := `SELECT id, order_id, from_status, to_status, actor, reason, created_at FROM order_events`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += fmt.Sprintf(" ORDER BY id DESC LIMIT $%d", argIdx)
	args = append(args, limit+1)

	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to fetch audit logs",
		))
		return
	}
	defer rows.Close()

	type auditLogItem struct {
		ID         int64     `json:"id"`
		OrderID    string    `json:"order_id"`
		FromStatus *string   `json:"from_status"`
		ToStatus   string    `json:"to_status"`
		Actor      string    `json:"actor"`
		Reason     string    `json:"reason"`
		CreatedAt  time.Time `json:"created_at"`
	}

	logs := make([]auditLogItem, 0)
	for rows.Next() {
		var item auditLogItem
		if err := rows.Scan(&item.ID, &item.OrderID, &item.FromStatus, &item.ToStatus, &item.Actor, &item.Reason, &item.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, domain.NewProblem(
				http.StatusInternalServerError, domain.CodeInternalServerError, "Scan Error", "Failed to scan audit log",
			))
			return
		}
		logs = append(logs, item)
	}

	var nextCursor *string
	if len(logs) > limit {
		logs = logs[:limit]
		lastIDStr := strconv.FormatInt(logs[len(logs)-1].ID, 10)
		nextCursor = &lastIDStr
	}

	c.JSON(http.StatusOK, gin.H{
		"data": logs,
		"pagination": gin.H{
			"next_cursor": nextCursor,
			"limit":       limit,
		},
	})
}

type SettlementRecord struct {
	OrderID   string `json:"order_id" binding:"required"`
	Amount    int64  `json:"amount" binding:"required"`
	Reference string `json:"reference"`
}

type ReconciliationReq struct {
	SettlementDate string             `json:"settlement_date" binding:"required"` // YYYY-MM-DD
	Records        []SettlementRecord `json:"records" binding:"required"`
}

// ReconcilePayments POST /api/v1/admin/reconciliation
func (h *AdminHandler) ReconcilePayments(c *gin.Context) {
	var req ReconciliationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, domain.NewProblem(
			http.StatusBadRequest, domain.CodeValidationFailed, "Validation Failed", err.Error(),
		))
		return
	}

	ctx := c.Request.Context()

	type dbOrderInfo struct {
		Total  int64
		Status string
	}
	dbOrders := make(map[string]dbOrderInfo)

	rows, err := h.db.Query(ctx, `
		SELECT id, total, status
		FROM orders
		WHERE DATE(created_at AT TIME ZONE 'UTC') = $1::date
	`, req.SettlementDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to query orders for reconciliation",
		))
		return
	}
	for rows.Next() {
		var id, st string
		var tot int64
		if err := rows.Scan(&id, &tot, &st); err == nil {
			dbOrders[id] = dbOrderInfo{Total: tot, Status: st}
		}
	}
	rows.Close()

	matched := make([]gin.H, 0)
	amountMismatch := make([]gin.H, 0)
	missingInDB := make([]gin.H, 0)
	seenInSettlement := make(map[string]bool)

	for _, rec := range req.Records {
		seenInSettlement[rec.OrderID] = true
		info, exists := dbOrders[rec.OrderID]
		if !exists {
			// ลองค้นหาเผื่อเป็นออเดอร์ข้ามวัน
			err := h.db.QueryRow(ctx, `SELECT total, status FROM orders WHERE id = $1`, rec.OrderID).Scan(&info.Total, &info.Status)
			if err != nil {
				missingInDB = append(missingInDB, gin.H{
					"order_id":          rec.OrderID,
					"settlement_amount": rec.Amount,
					"reference":         rec.Reference,
				})
				continue
			}
		}

		if info.Total == rec.Amount {
			matched = append(matched, gin.H{
				"order_id": rec.OrderID,
				"amount":   rec.Amount,
				"status":   info.Status,
			})
		} else {
			amountMismatch = append(amountMismatch, gin.H{
				"order_id":          rec.OrderID,
				"expected_amount":   info.Total,
				"settlement_amount": rec.Amount,
				"difference":        rec.Amount - info.Total,
				"status":            info.Status,
			})
		}
	}

	unsettledInGateway := make([]gin.H, 0)
	for id, info := range dbOrders {
		if !seenInSettlement[id] && (info.Status == "PAID" || info.Status == "IN_QUEUE" || info.Status == "IN_PROGRESS" || info.Status == "COMPLETED") {
			unsettledInGateway = append(unsettledInGateway, gin.H{
				"order_id": id,
				"amount":   info.Total,
				"status":   info.Status,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"settlement_date": req.SettlementDate,
		"summary": gin.H{
			"total_records":         len(req.Records),
			"matched_count":         len(matched),
			"mismatch_count":        len(amountMismatch),
			"missing_in_db_count":   len(missingInDB),
			"unsettled_in_gw_count": len(unsettledInGateway),
		},
		"matched":              matched,
		"amount_mismatch":      amountMismatch,
		"missing_in_db":        missingInDB,
		"unsettled_in_gateway": unsettledInGateway,
	})
}

// TestReset POST /api/v1/_test/reset (เฉพาะ APP_ENV=test เท่านั้น)
func (h *AdminHandler) TestReset(c *gin.Context) {
	if os.Getenv("APP_ENV") != "test" {
		c.JSON(http.StatusNotFound, domain.NewProblem(
			http.StatusNotFound, domain.CodeNotFound, "Not Found", "Endpoint not available in current environment",
		))
		return
	}

	ctx := c.Request.Context()
	tx, err := h.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Transaction Error", err.Error(),
		))
		return
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		TRUNCATE TABLE
			order_events,
			order_items,
			fulfillment_attempts,
			refunds,
			voucher_usages,
			webhook_events,
			idempotency_keys,
			orders
		RESTART IDENTITY CASCADE
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Reset Error", err.Error(),
		))
		return
	}

	// รีเซ็ตสต็อก Flash Sale และโควตา Voucher กลับเป็นค่าตั้งต้น
	_, _ = tx.Exec(ctx, `UPDATE products SET stock = 100, updated_at = NOW() WHERE sku = 'FLASH-SKIN-01'`)
	_, _ = tx.Exec(ctx, `UPDATE vouchers SET used_count = 0, is_active = TRUE, updated_at = NOW()`)

	if err := tx.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Commit Error", err.Error(),
		))
		return
	}

	// ล้าง Redis Stream
	_ = h.redis.Del(ctx, FulfillmentStreamKey).Err()

	c.JSON(http.StatusOK, gin.H{"status": "reset_completed"})
}

func encodeOrderCursor(createdAt time.Time, id string) string {
	raw := fmt.Sprintf("%s|%s", createdAt.UTC().Format(time.RFC3339Nano), id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeOrderCursor(cursor string) (time.Time, string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", err
	}
	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("invalid cursor format")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", err
	}
	return t, parts[1], nil
}
