package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/inblack/topup-backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OrderHandler struct {
	db *pgxpool.Pool
}

func NewOrderHandler(db *pgxpool.Pool) *OrderHandler {
	return &OrderHandler{db: db}
}

type CreateOrderItemReq struct {
	SKU      string `json:"sku" binding:"required"`
	Quantity int    `json:"quantity" binding:"required,min=1"`
}

type CreateOrderReq struct {
	CustomerEmail string               `json:"customer_email" binding:"required,email"`
	GameUID       string               `json:"game_uid" binding:"required"`
	PaymentMethod string               `json:"payment_method" binding:"required"`
	VoucherCode   *string              `json:"voucher_code"`
	Items         []CreateOrderItemReq `json:"items" binding:"required,min=1"`
}

type OrderItemResp struct {
	SKU           string `json:"sku"`
	ProductNameTH string `json:"product_name_th"`
	ProductNameEN string `json:"product_name_en"`
	UnitPrice     int64  `json:"unit_price"`
	Quantity      int    `json:"quantity"`
	TotalPrice    int64  `json:"total_price"`
}

type OrderResp struct {
	ID            string          `json:"id"`
	CustomerEmail string          `json:"customer_email"`
	GameUID       string          `json:"game_uid"`
	PaymentMethod string          `json:"payment_method"`
	VoucherCode   *string         `json:"voucher_code"`
	Subtotal      int64           `json:"subtotal"`
	Discount      int64           `json:"discount"`
	Net           int64           `json:"net"`
	Fee           int64           `json:"fee"`
	Total         int64           `json:"total"`
	VAT           int64           `json:"vat"`
	Status        string          `json:"status"`
	ExpiresAt     time.Time       `json:"expires_at"`
	CreatedAt     time.Time       `json:"created_at"`
	Items         []OrderItemResp `json:"items"`
}

type TrackOrderReq struct {
	OrderID       string `json:"order_id" binding:"required"`
	CustomerEmail string `json:"customer_email" binding:"required,email"`
}

type OrderEventResp struct {
	ID         int64     `json:"id"`
	FromStatus *string   `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	Actor      string    `json:"actor"`
	Reason     string    `json:"reason"`
	CreatedAt  time.Time `json:"created_at"`
}

type TrackOrderResp struct {
	OrderResp
	Events []OrderEventResp `json:"events"`
}

// CreateOrder POST /api/v1/orders
func (h *OrderHandler) CreateOrder(c *gin.Context) {
	ctx := c.Request.Context()

	// ตรวจสอบ Idempotency-Key Header
	idemKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idemKey == "" {
		c.JSON(http.StatusBadRequest, domain.NewProblem(
			http.StatusBadRequest, domain.CodeValidationFailed, "Missing Idempotency-Key", "Idempotency-Key header is required",
		))
		return
	}

	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, domain.NewProblem(
			http.StatusBadRequest, domain.CodeValidationFailed, "Invalid Body", "Cannot read request body",
		))
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(rawBody))

	var req CreateOrderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, domain.NewProblem(
			http.StatusBadRequest, domain.CodeValidationFailed, "Validation Failed", err.Error(),
		))
		return
	}

	req.CustomerEmail = strings.ToLower(strings.TrimSpace(req.CustomerEmail))
	req.GameUID = strings.TrimSpace(req.GameUID)
	req.PaymentMethod = strings.ToUpper(strings.TrimSpace(req.PaymentMethod))

	paymentMethod := domain.PaymentMethod(req.PaymentMethod)
	if paymentMethod != domain.PaymentPromptPay &&
		paymentMethod != domain.PaymentTrueMoney &&
		paymentMethod != domain.PaymentCreditCard {
		c.JSON(http.StatusBadRequest, domain.NewProblem(
			http.StatusBadRequest, domain.CodeValidationFailed, "Invalid Payment Method", "Supported methods: PROMPTPAY, TRUEMONEY, CREDIT_CARD",
		))
		return
	}

	// รวมจำนวนสินค้าหากส่ง SKU ซ้ำกันใน array
	qtyBySKU := make(map[string]int)
	for _, item := range req.Items {
		sku := strings.TrimSpace(item.SKU)
		qtyBySKU[sku] += item.Quantity
	}

	// เรียงลำดับ SKU เพื่อป้องกัน Deadlock ตอน SELECT ... FOR UPDATE
	skus := make([]string, 0, len(qtyBySKU))
	for sku := range qtyBySKU {
		skus = append(skus, sku)
	}
	sort.Strings(skus)

	// สร้าง Canonical Hash ของ Request สำหรับเช็ก Idempotency
	reqHash := hashRequest(req, skus, qtyBySKU)

	// ทำ Database Transaction
	tx, err := h.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Transaction Error", "Failed to start transaction",
		))
		return
	}
	defer tx.Rollback(ctx)

	// เช็ก Idempotency Key ภายใน Transaction (ล็อกด้วย pg_advisory_xact_lock กันยิงพร้อมกันเสี้ยววินาที)
	_, _ = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", idemKey)

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
				http.StatusUnprocessableEntity, domain.CodeIdempotencyKeyReused, "Idempotency Key Reused", "Idempotency-Key was already used with a different request payload",
			))
			return
		}
		c.Data(existingStatus, "application/json; charset=utf-8", existingBody)
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to check idempotency key",
		))
		return
	}

	// ล็อกและตรวจสอบสินค้าทีละรายการตามลำดับ SKU
	type lockedProduct struct {
		id            string
		sku           string
		nameTH        string
		nameEN        string
		price         int64
		stock         *int
		limitPerOrder *int
		limitPerEmail *int
		qty           int
	}

	lockedProducts := make([]lockedProduct, 0, len(skus))
	priceInputs := make([]domain.OrderItemInput, 0, len(skus))

	for _, sku := range skus {
		qty := qtyBySKU[sku]
		var p lockedProduct
		p.qty = qty

		err := tx.QueryRow(ctx, `
			SELECT id, sku, name_th, name_en, price, stock, limit_per_order, limit_per_email
			FROM products
			WHERE sku = $1
			FOR UPDATE
		`, sku).Scan(&p.id, &p.sku, &p.nameTH, &p.nameEN, &p.price, &p.stock, &p.limitPerOrder, &p.limitPerEmail)

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				c.JSON(http.StatusNotFound, domain.NewProblem(
					http.StatusNotFound, domain.CodeNotFound, "Product Not Found", fmt.Sprintf("Product SKU %s does not exist", sku),
				))
				return
			}
			c.JSON(http.StatusInternalServerError, domain.NewProblem(
				http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to lock product",
			))
			return
		}

		// เช็ก limit_per_order
		if p.limitPerOrder != nil && qty > *p.limitPerOrder {
			c.JSON(http.StatusUnprocessableEntity, domain.NewProblem(
				http.StatusUnprocessableEntity, domain.CodePerOrderLimitExceeded, "Per-Order Limit Exceeded",
				fmt.Sprintf("SKU %s allows maximum %d per order", sku, *p.limitPerOrder),
			))
			return
		}

		// เช็ก limit_per_email (นับออเดอร์ที่ยังไม่ถูกยกเลิก/หมดอายุ/คืนเงิน)
		if p.limitPerEmail != nil {
			var purchasedQty int
			err := tx.QueryRow(ctx, `
				SELECT COALESCE(SUM(oi.quantity), 0)
				FROM order_items oi
				JOIN orders o ON o.id = oi.order_id
				WHERE oi.product_id = $1
				  AND LOWER(o.customer_email) = $2
				  AND o.status NOT IN ('EXPIRED', 'PAYMENT_FAILED', 'FAILED', 'REFUNDED')
			`, p.id, req.CustomerEmail).Scan(&purchasedQty)
			if err != nil {
				c.JSON(http.StatusInternalServerError, domain.NewProblem(
					http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to check customer product quota",
				))
				return
			}
			if purchasedQty+qty > *p.limitPerEmail {
				c.JSON(http.StatusConflict, domain.NewProblem(
					http.StatusConflict, domain.CodePerCustomerLimit, "Per-Customer Limit Exceeded",
					fmt.Sprintf("Customer has reached purchase limit (%d) for SKU %s", *p.limitPerEmail, sku),
				))
				return
			}
		}

		// เช็กและตัด Stock (กรณีสินค้ามีจำกัด)
		if p.stock != nil {
			if *p.stock < qty {
				c.JSON(http.StatusConflict, domain.NewProblem(
					http.StatusConflict, domain.CodeOutOfStock, "Out of Stock",
					fmt.Sprintf("Insufficient stock for SKU %s", sku),
				))
				return
			}
			if _, err := tx.Exec(ctx, `
				UPDATE products SET stock = stock - $1, updated_at = NOW() WHERE id = $2
			`, qty, p.id); err != nil {
				c.JSON(http.StatusInternalServerError, domain.NewProblem(
					http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to deduct stock",
				))
				return
			}
		}

		lockedProducts = append(lockedProducts, p)
		priceInputs = append(priceInputs, domain.OrderItemInput{
			UnitPrice: p.price,
			Quantity:  int64(qty),
		})
	}

	// ล็อกและตรวจสอบ Voucher (หากมีการระบุโค้ดส่วนลด)
	var voucherID string
	var voucherCodePtr *string
	var voucherRule *domain.VoucherRule

	if req.VoucherCode != nil && strings.TrimSpace(*req.VoucherCode) != "" {
		code := strings.ToUpper(strings.TrimSpace(*req.VoucherCode))
		voucherCodePtr = &code

		var vType string
		var discountVal, minSubtotal int64
		var maxDiscount *int64
		var usageLimit, limitPerEmail *int
		var usedCount int
		var isActive bool

		err := tx.QueryRow(ctx, `
			SELECT id, type, discount_value, max_discount, min_subtotal, usage_limit, used_count, limit_per_email, is_active
			FROM vouchers
			WHERE code = $1
			FOR UPDATE
		`, code).Scan(&voucherID, &vType, &discountVal, &maxDiscount, &minSubtotal, &usageLimit, &usedCount, &limitPerEmail, &isActive)

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				c.JSON(http.StatusUnprocessableEntity, domain.NewProblem(
					http.StatusUnprocessableEntity, domain.CodeVoucherInvalid, "Invalid Voucher", "Voucher code does not exist",
				))
				return
			}
			c.JSON(http.StatusInternalServerError, domain.NewProblem(
				http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to lock voucher",
			))
			return
		}

		if !isActive {
			c.JSON(http.StatusUnprocessableEntity, domain.NewProblem(
				http.StatusUnprocessableEntity, domain.CodeVoucherInvalid, "Voucher Inactive", "This voucher is no longer active",
			))
			return
		}

		if usageLimit != nil && usedCount >= *usageLimit {
			c.JSON(http.StatusConflict, domain.NewProblem(
				http.StatusConflict, domain.CodeVoucherExhausted, "Voucher Exhausted", "This voucher has reached its global usage limit",
			))
			return
		}

		if limitPerEmail != nil {
			var emailUsed int
			err := tx.QueryRow(ctx, `
				SELECT COUNT(*)
				FROM voucher_usages
				WHERE voucher_id = $1 AND LOWER(customer_email) = $2 AND is_active = TRUE
			`, voucherID, req.CustomerEmail).Scan(&emailUsed)
			if err != nil {
				c.JSON(http.StatusInternalServerError, domain.NewProblem(
					http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to check voucher usage per email",
				))
				return
			}
			if emailUsed >= *limitPerEmail {
				c.JSON(http.StatusConflict, domain.NewProblem(
					http.StatusConflict, domain.CodePerCustomerLimit, "Voucher Limit Per Email Exceeded", "Customer has already used this voucher",
				))
				return
			}
		}

		voucherRule = &domain.VoucherRule{
			Type:          domain.DiscountType(vType),
			DiscountValue: discountVal,
			MaxDiscount:   maxDiscount,
			MinSubtotal:   minSubtotal,
		}
	}

	// คำนวณราคาสุทธิจาก Core Domain
	breakdown, err := domain.CalculatePrice(priceInputs, voucherRule, paymentMethod)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, domain.NewProblem(
			http.StatusUnprocessableEntity, domain.CodeValidationFailed, "Price Calculation Failed", err.Error(),
		))
		return
	}

	// เช็กเงื่อนไขยอดขั้นต่ำของ Voucher (ถ้ามียอดไม่ถึง ให้ปฏิเสธชัดเจน)
	if voucherRule != nil && breakdown.Subtotal < voucherRule.MinSubtotal {
		c.JSON(http.StatusUnprocessableEntity, domain.NewProblem(
			http.StatusUnprocessableEntity, domain.CodeVoucherMinSubtotal, "Minimum Subtotal Not Met",
			fmt.Sprintf("Voucher requires minimum subtotal of %d satang", voucherRule.MinSubtotal),
		))
		return
	}

	// สร้างเลขออเดอร์และบันทึกข้อมูลลงตาราง orders, order_items, voucher_usages, order_events
	orderID, err := domain.GenerateOrderID()
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "ID Generation Failed", "Failed to generate order ID",
		))
		return
	}

	now := time.Now().UTC()
	expiresAt := now.Add(15 * time.Minute)

	_, err = tx.Exec(ctx, `
		INSERT INTO orders (
			id, customer_email, game_uid, payment_method, voucher_code,
			subtotal, discount, net, fee, total, vat, status, expires_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10, $11, 'PENDING_PAYMENT', $12, $13, $13
		)
	`, orderID, req.CustomerEmail, req.GameUID, req.PaymentMethod, voucherCodePtr,
		breakdown.Subtotal, breakdown.Discount, breakdown.Net, breakdown.Fee, breakdown.Total, breakdown.VAT,
		expiresAt, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to insert order",
		))
		return
	}

	itemResps := make([]OrderItemResp, 0, len(lockedProducts))
	for _, p := range lockedProducts {
		totalPrice := p.price * int64(p.qty)
		_, err = tx.Exec(ctx, `
			INSERT INTO order_items (order_id, product_id, sku, product_name_th, product_name_en, unit_price, quantity, total_price)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, orderID, p.id, p.sku, p.nameTH, p.nameEN, p.price, p.qty, totalPrice)
		if err != nil {
			c.JSON(http.StatusInternalServerError, domain.NewProblem(
				http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to insert order item",
			))
			return
		}

		itemResps = append(itemResps, OrderItemResp{
			SKU:           p.sku,
			ProductNameTH: p.nameTH,
			ProductNameEN: p.nameEN,
			UnitPrice:     p.price,
			Quantity:      p.qty,
			TotalPrice:    totalPrice,
		})
	}

	if voucherID != "" {
		if _, err := tx.Exec(ctx, `UPDATE vouchers SET used_count = used_count + 1, updated_at = NOW() WHERE id = $1`, voucherID); err != nil {
			c.JSON(http.StatusInternalServerError, domain.NewProblem(
				http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to update voucher count",
			))
			return
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO voucher_usages (voucher_id, order_id, customer_email, is_active)
			VALUES ($1, $2, $3, TRUE)
		`, voucherID, orderID, req.CustomerEmail); err != nil {
			c.JSON(http.StatusInternalServerError, domain.NewProblem(
				http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to record voucher usage",
			))
			return
		}
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO order_events (order_id, from_status, to_status, actor, reason)
		VALUES ($1, NULL, 'PENDING_PAYMENT', 'customer', 'Order created and awaiting payment')
	`, orderID); err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to record initial order event",
		))
		return
	}

	// Response JSON และบันทึกลง idempotency_keys ก่อน Commit
	respPayload := gin.H{
		"data": OrderResp{
			ID:            orderID,
			CustomerEmail: req.CustomerEmail,
			GameUID:       req.GameUID,
			PaymentMethod: req.PaymentMethod,
			VoucherCode:   voucherCodePtr,
			Subtotal:      breakdown.Subtotal,
			Discount:      breakdown.Discount,
			Net:           breakdown.Net,
			Fee:           breakdown.Fee,
			Total:         breakdown.Total,
			VAT:           breakdown.VAT,
			Status:        string(domain.StatusPendingPayment),
			ExpiresAt:     expiresAt,
			CreatedAt:     now,
			Items:         itemResps,
		},
	}

	respBytes, err := json.Marshal(respPayload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Serialization Error", "Failed to serialize response",
		))
		return
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO idempotency_keys (idempotency_key, request_hash, response_status, response_body)
		VALUES ($1, $2, $3, $4::jsonb)
	`, idemKey, reqHash, http.StatusCreated, string(respBytes)); err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to save idempotency key",
		))
		return
	}

	if err := tx.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Commit Error", "Failed to commit order transaction",
		))
		return
	}

	c.Data(http.StatusCreated, "application/json; charset=utf-8", respBytes)
}

// GetOrder GET /api/v1/orders/:id
func (h *OrderHandler) GetOrder(c *gin.Context) {
	orderID := strings.TrimSpace(c.Param("id"))
	if err := domain.ValidateOrderID(orderID); err != nil {
		c.JSON(http.StatusBadRequest, domain.NewProblem(
			http.StatusBadRequest, domain.CodeValidationFailed, "Invalid Order ID", "Order ID format or checksum is invalid",
		))
		return
	}

	var o OrderResp
	err := h.db.QueryRow(c.Request.Context(), `
		SELECT id, customer_email, game_uid, payment_method, voucher_code,
		       subtotal, discount, net, fee, total, vat, status, expires_at, created_at
		FROM orders
		WHERE id = $1
	`, orderID).Scan(
		&o.ID, &o.CustomerEmail, &o.GameUID, &o.PaymentMethod, &o.VoucherCode,
		&o.Subtotal, &o.Discount, &o.Net, &o.Fee, &o.Total, &o.VAT,
		&o.Status, &o.ExpiresAt, &o.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, domain.NewProblem(
				http.StatusNotFound, domain.CodeNotFound, "Order Not Found", "No order found with the given ID",
			))
			return
		}
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to fetch order",
		))
		return
	}

	rows, err := h.db.Query(c.Request.Context(), `
		SELECT sku, product_name_th, product_name_en, unit_price, quantity, total_price
		FROM order_items
		WHERE order_id = $1
	`, orderID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to fetch order items",
		))
		return
	}
	defer rows.Close()

	o.Items = make([]OrderItemResp, 0)
	for rows.Next() {
		var item OrderItemResp
		if err := rows.Scan(&item.SKU, &item.ProductNameTH, &item.ProductNameEN, &item.UnitPrice, &item.Quantity, &item.TotalPrice); err != nil {
			c.JSON(http.StatusInternalServerError, domain.NewProblem(
				http.StatusInternalServerError, domain.CodeInternalServerError, "Scan Error", "Failed to read order item",
			))
			return
		}
		o.Items = append(o.Items, item)
	}

	c.JSON(http.StatusOK, gin.H{"data": o})
}

func hashRequest(req CreateOrderReq, sortedSKUs []string, qtyBySKU map[string]int) string {
	voucher := ""
	if req.VoucherCode != nil {
		voucher = strings.ToUpper(strings.TrimSpace(*req.VoucherCode))
	}
	var sb strings.Builder
	sb.WriteString(req.CustomerEmail + "|" + req.GameUID + "|" + req.PaymentMethod + "|" + voucher + "|")
	for _, sku := range sortedSKUs {
		sb.WriteString(fmt.Sprintf("%s:%d,", sku, qtyBySKU[sku]))
	}
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}

// TrackOrder POST /api/v1/orders/track
func (h *OrderHandler) TrackOrder(c *gin.Context) {
	var req TrackOrderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, domain.NewProblem(
			http.StatusBadRequest, domain.CodeValidationFailed, "Validation Failed", err.Error(),
		))
		return
	}

	orderID := strings.TrimSpace(req.OrderID)
	email := strings.ToLower(strings.TrimSpace(req.CustomerEmail))

	if err := domain.ValidateOrderID(orderID); err != nil {
		c.JSON(http.StatusNotFound, domain.NewProblem(
			http.StatusNotFound, domain.CodeNotFound, "Order Not Found", "No matching order found for the provided ID and email",
		))
		return
	}

	var resp TrackOrderResp
	err := h.db.QueryRow(c.Request.Context(), `
		SELECT id, customer_email, game_uid, payment_method, voucher_code,
		       subtotal, discount, net, fee, total, vat, status, expires_at, created_at
		FROM orders
		WHERE id = $1 AND LOWER(customer_email) = $2
	`, orderID, email).Scan(
		&resp.ID, &resp.CustomerEmail, &resp.GameUID, &resp.PaymentMethod, &resp.VoucherCode,
		&resp.Subtotal, &resp.Discount, &resp.Net, &resp.Fee, &resp.Total, &resp.VAT,
		&resp.Status, &resp.ExpiresAt, &resp.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, domain.NewProblem(
				http.StatusNotFound, domain.CodeNotFound, "Order Not Found", "No matching order found for the provided ID and email",
			))
			return
		}
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to lookup order",
		))
		return
	}

	// ดึงรายการสินค้า
	itemRows, err := h.db.Query(c.Request.Context(), `
		SELECT sku, product_name_th, product_name_en, unit_price, quantity, total_price
		FROM order_items
		WHERE order_id = $1
	`, orderID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to fetch order items",
		))
		return
	}
	defer itemRows.Close()

	resp.Items = make([]OrderItemResp, 0)
	for itemRows.Next() {
		var item OrderItemResp
		if err := itemRows.Scan(&item.SKU, &item.ProductNameTH, &item.ProductNameEN, &item.UnitPrice, &item.Quantity, &item.TotalPrice); err != nil {
			c.JSON(http.StatusInternalServerError, domain.NewProblem(
				http.StatusInternalServerError, domain.CodeInternalServerError, "Scan Error", "Failed to scan order item",
			))
			return
		}
		resp.Items = append(resp.Items, item)
	}

	// ดึง Timeline ประวัติการเปลี่ยนสถานะ
	eventRows, err := h.db.Query(c.Request.Context(), `
		SELECT id, from_status, to_status, actor, reason, created_at
		FROM order_events
		WHERE order_id = $1
		ORDER BY id ASC
	`, orderID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to fetch order events",
		))
		return
	}
	defer eventRows.Close()

	resp.Events = make([]OrderEventResp, 0)
	for eventRows.Next() {
		var ev OrderEventResp
		if err := eventRows.Scan(&ev.ID, &ev.FromStatus, &ev.ToStatus, &ev.Actor, &ev.Reason, &ev.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, domain.NewProblem(
				http.StatusInternalServerError, domain.CodeInternalServerError, "Scan Error", "Failed to scan order event",
			))
			return
		}
		resp.Events = append(resp.Events, ev)
	}

	c.JSON(http.StatusOK, gin.H{"data": resp})
}

// StreamOrderEvents GET /api/v1/orders/:id/events (SSE)
func (h *OrderHandler) StreamOrderEvents(c *gin.Context) {
	orderID := strings.TrimSpace(c.Param("id"))
	if err := domain.ValidateOrderID(orderID); err != nil {
		c.JSON(http.StatusBadRequest, domain.NewProblem(
			http.StatusBadRequest, domain.CodeValidationFailed, "Invalid Order ID", "Order ID format or checksum is invalid",
		))
		return
	}

	// ตรวจสอบว่ามีออเดอร์อยู่จริงก่อนเปิด Stream
	var exists bool
	if err := h.db.QueryRow(c.Request.Context(), `SELECT EXISTS(SELECT 1 FROM orders WHERE id = $1)`, orderID).Scan(&exists); err != nil || !exists {
		c.JSON(http.StatusNotFound, domain.NewProblem(
			http.StatusNotFound, domain.CodeNotFound, "Order Not Found", "No order found with the given ID",
		))
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no") // ปิด buffer ของ Nginx เพื่อให้ส่งข้อมูลทันที
	c.Writer.Flush()

	var lastEventID int64 = 0
	ctx := c.Request.Context()

	sendNewEvents := func() (bool, error) {
		rows, err := h.db.Query(ctx, `
			SELECT id, from_status, to_status, actor, reason, created_at
			FROM order_events
			WHERE order_id = $1 AND id > $2
			ORDER BY id ASC
		`, orderID, lastEventID)
		if err != nil {
			return false, err
		}
		defer rows.Close()

		isTerminal := false
		for rows.Next() {
			var ev OrderEventResp
			if err := rows.Scan(&ev.ID, &ev.FromStatus, &ev.ToStatus, &ev.Actor, &ev.Reason, &ev.CreatedAt); err != nil {
				return false, err
			}
			lastEventID = ev.ID

			payload, _ := json.Marshal(ev)
			fmt.Fprintf(c.Writer, "id: %d\nevent: status_changed\ndata: %s\n\n", ev.ID, string(payload))
			c.Writer.Flush()

			// ปิด stream อัตโนมัติเมื่อถึงสถานะปลายทางสมบูรณ์
			if ev.ToStatus == string(domain.StatusCompleted) || ev.ToStatus == string(domain.StatusRefunded) {
				isTerminal = true
			}
		}
		return isTerminal, nil
	}

	// ส่งเหตุการณ์ที่มีอยู่แล้วทันทีเมื่อเชื่อมต่อ
	if done, err := sendNewEvents(); err != nil || done {
		return
	}

	pollTicker := time.NewTicker(1 * time.Second)
	pingTicker := time.NewTicker(15 * time.Second)
	defer pollTicker.Stop()
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-pollTicker.C:
			if done, err := sendNewEvents(); err != nil || done {
				return
			}
		case <-pingTicker.C:
			fmt.Fprintf(c.Writer, ": ping\n\n")
			c.Writer.Flush()
		}
	}
}
