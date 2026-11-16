package test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type testEnv struct {
	baseURL       string
	db            *pgxpool.Pool
	webhookSecret string
	adminToken    string
}

func loadDotEnv(paths ...string) {
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			if os.Getenv(k) == "" {
				_ = os.Setenv(k, v)
			}
		}
		return
	}
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()
	loadDotEnv("../../.env", "../.env", ".env")

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Fatal("DATABASE_URL must be set in .env or environment")
	}

	secret := os.Getenv("PAYMENT_WEBHOOK_SECRET")
	if secret == "" {
		t.Fatal("PAYMENT_WEBHOOK_SECRET must be set in .env or environment")
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		t.Fatal("JWT_SECRET must be set in .env or environment")
	}

	baseURL := os.Getenv("TEST_API_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	ctx := context.Background()
	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to connect db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	resetTestDatabase(ctx, t, db)
	adminToken := mintSuperAdminToken(t, jwtSecret)

	return &testEnv{
		baseURL:       baseURL,
		db:            db,
		webhookSecret: secret,
		adminToken:    adminToken,
	}
}

func resetTestDatabase(ctx context.Context, t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	tables := []string{
		"refunds",
		"voucher_usages",
		"fulfillment_attempts",
		"order_events",
		"order_items",
		"webhook_events",
		"idempotency_keys",
		"orders",
	}
	for _, tbl := range tables {
		if _, err := db.Exec(ctx, fmt.Sprintf(`TRUNCATE TABLE %s RESTART IDENTITY CASCADE`, tbl)); err != nil {
			t.Fatalf("failed to truncate %s: %v", tbl, err)
		}
	}

	if _, err := db.Exec(ctx, `UPDATE products SET stock = NULL WHERE sku != 'FLASH-SKIN-01'`); err != nil {
		t.Fatalf("failed to reset unlimited products: %v", err)
	}
	if _, err := db.Exec(ctx, `UPDATE products SET stock = 100 WHERE sku = 'FLASH-SKIN-01'`); err != nil {
		t.Fatalf("failed to reset FLASH-SKIN-01 stock: %v", err)
	}
	if _, err := db.Exec(ctx, `UPDATE vouchers SET used_count = 0, is_active = TRUE`); err != nil {
		t.Fatalf("failed to reset vouchers: %v", err)
	}
}

func mintSuperAdminToken(t *testing.T, jwtSecret string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"email": "superadmin@inblack.gg",
		"role":  "superadmin",
		"exp":   time.Now().Add(1 * time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		t.Fatalf("failed to sign admin JWT: %v", err)
	}
	return signed
}

func (e *testEnv) createOrder(idemKey, sku string, qty int, email, voucher string) (string, int64, int, string) {
	payload := map[string]interface{}{
		"game_uid":       "UID-999888777",
		"customer_email": email,
		"payment_method": "PROMPTPAY",
		"items": []map[string]interface{}{
			{"sku": sku, "quantity": qty},
		},
	}
	if voucher != "" {
		payload["voucher_code"] = voucher
	}
	raw, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, e.baseURL+"/api/v1/orders", bytes.NewReader(raw))
	if err != nil {
		return "", 0, 0, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idemKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, 0, err.Error()
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return "", 0, resp.StatusCode, string(bodyBytes)
	}

	var res struct {
		ID    string `json:"id"`
		Total int64  `json:"total"`
		Data  struct {
			ID    string `json:"id"`
			Total int64  `json:"total"`
		} `json:"data"`
	}
	_ = json.Unmarshal(bodyBytes, &res)
	if res.ID != "" {
		return res.ID, res.Total, resp.StatusCode, ""
	}
	return res.Data.ID, res.Data.Total, resp.StatusCode, ""
}

func (e *testEnv) sendWebhook(eventID, eventType, orderID string, amount int64) int {
	payload := map[string]interface{}{
		"event_id":    eventID,
		"event_type":  eventType,
		"order_id":    orderID,
		"amount":      amount,
		"occurred_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	raw, _ := json.Marshal(payload)

	mac := hmac.New(sha256.New, []byte(e.webhookSecret))
	mac.Write(raw)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, _ := http.NewRequest(http.MethodPost, e.baseURL+"/api/v1/webhooks/payment", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", sig)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// ============================================================================
// INVARIANT 1: ห้าม Oversell — FLASH-SKIN-01 ขายได้ไม่เกิน 100 ชิ้น
// ============================================================================
func TestInvariant1_NoOversell(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	const concurrency = 500
	start := make(chan struct{})
	var wg sync.WaitGroup
	var createdCount int32
	var rejectedCount int32
	var sampleErr atomic.Value

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			email := fmt.Sprintf("buyer_%d@test.com", idx)
			idemKey := fmt.Sprintf("inv1_order_%d_%d", time.Now().UnixNano(), idx)
			_, _, status, errBody := env.createOrder(idemKey, "FLASH-SKIN-01", 1, email, "")
			if status == http.StatusCreated {
				atomic.AddInt32(&createdCount, 1)
			} else {
				atomic.AddInt32(&rejectedCount, 1)
				if sampleErr.Load() == nil && status != http.StatusConflict && status != http.StatusUnprocessableEntity {
					sampleErr.Store(fmt.Sprintf("status=%d body=%s", status, errBody))
				}
			}
		}(i)
	}

	close(start)
	wg.Wait()

	var remainingStock int
	if err := env.db.QueryRow(ctx, `SELECT stock FROM products WHERE sku = 'FLASH-SKIN-01'`).Scan(&remainingStock); err != nil {
		t.Fatalf("failed to query stock: %v", err)
	}

	if createdCount != 100 {
		t.Errorf("expected 100 created orders, got %d (sample err: %v)", createdCount, sampleErr.Load())
	}
	if rejectedCount != concurrency-100 {
		t.Errorf("expected %d rejected requests, got %d", concurrency-100, rejectedCount)
	}
	if remainingStock != 0 {
		t.Errorf("stock must be exactly 0 and never negative, got %d", remainingStock)
	}
}

// ============================================================================
// INVARIANT 2: ห้ามเติมเกมซ้ำ — 1 ออเดอร์ เติมเกมสำเร็จไม่เกิน 1 ครั้ง
// ============================================================================
func TestInvariant2_NoDuplicateFulfillment(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	orderID, total, status, errBody := env.createOrder("inv2_order_1", "DIAMOND-1000", 1, "idempotent@test.com", "")
	if status != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", status, errBody)
	}

	const webhookStorm = 10
	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < webhookStorm; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			eventID := "evt_fixed_idempotent_key"
			if idx >= 5 {
				eventID = fmt.Sprintf("evt_retry_new_id_%d", idx)
			}
			_ = env.sendWebhook(eventID, "payment.succeeded", orderID, total)
		}(i)
	}

	close(start)
	wg.Wait()

	time.Sleep(2 * time.Second)

	var orderStatus string
	if err := env.db.QueryRow(ctx, `SELECT status FROM orders WHERE id = $1`, orderID).Scan(&orderStatus); err != nil {
		t.Fatalf("failed to query order status: %v", err)
	}
	if orderStatus != "COMPLETED" {
		t.Errorf("expected order status COMPLETED, got %s", orderStatus)
	}

	var fulfillmentCount int
	if err := env.db.QueryRow(ctx, `SELECT COUNT(*) FROM fulfillment_attempts WHERE order_id = $1`, orderID).Scan(&fulfillmentCount); err != nil {
		t.Fatalf("failed to query fulfillment_attempts: %v", err)
	}
	if fulfillmentCount != 1 {
		t.Errorf("order must have at most 1 fulfillment attempt record, got %d", fulfillmentCount)
	}

	var completedTransitionCount int
	if err := env.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM order_events 
		WHERE order_id = $1 AND to_status = 'COMPLETED'
	`, orderID).Scan(&completedTransitionCount); err != nil {
		t.Fatalf("failed to query order_events: %v", err)
	}
	if completedTransitionCount != 1 {
		t.Errorf("order must transition to COMPLETED at most once, got %d", completedTransitionCount)
	}
}

// ============================================================================
// INVARIANT 3: ห้ามคืนเงินเกินยอดจ่าย — แม้ Finance 2 คนกด Refund พร้อมกัน
// ============================================================================
func TestInvariant3_NoOverRefund(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	orderID, total, status, errBody := env.createOrder("inv3_order_1", "DIAMOND-1000", 1, "refund@test.com", "")
	if status != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", status, errBody)
	}
	if whStatus := env.sendWebhook("evt_ref_01", "payment.succeeded", orderID, total); whStatus != http.StatusOK {
		t.Fatalf("expected webhook 200 OK, got %d", whStatus)
	}

	time.Sleep(1500 * time.Millisecond)

	const concurrentAdmins = 10
	start := make(chan struct{})
	var wg sync.WaitGroup
	var successRefunds int32

	for i := 0; i < concurrentAdmins; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			body, _ := json.Marshal(map[string]interface{}{
				"amount": total,
				"reason": "Customer requested refund",
			})
			req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/admin/orders/%s/refund", env.baseURL, orderID), bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", fmt.Sprintf("ref_idem_%d_%d", time.Now().UnixNano(), idx))
			req.Header.Set("Authorization", "Bearer "+env.adminToken)

			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				if resp.StatusCode == http.StatusOK {
					atomic.AddInt32(&successRefunds, 1)
				}
				_ = resp.Body.Close()
			}
		}(i)
	}

	close(start)
	wg.Wait()

	if successRefunds != 1 {
		t.Errorf("only 1 concurrent refund request may succeed, got %d", successRefunds)
	}

	var totalRefunded int64
	if err := env.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount), 0) FROM refunds WHERE order_id = $1
	`, orderID).Scan(&totalRefunded); err != nil {
		t.Fatalf("failed to query refunds: %v", err)
	}
	if totalRefunded > total {
		t.Errorf("total refunded amount (%d) must never exceed paid amount (%d)", totalRefunded, total)
	}
}

// ============================================================================
// INVARIANT 4: โค้ดส่วนลดห้ามถูกใช้เกินสิทธิ — ทั้งเพดานรวมและเพดานต่ออีเมล
// ============================================================================
func TestInvariant4_VoucherLimitsStrict(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	if _, err := env.db.Exec(ctx, `
		INSERT INTO vouchers (id, code, type, discount_value, usage_limit, limit_per_email, used_count, is_active)
		VALUES (gen_random_uuid(), 'STRICT5', 'FIXED', 1000, 5, 1, 0, TRUE)
		ON CONFLICT (code) DO UPDATE SET usage_limit = 5, limit_per_email = 1, used_count = 0, is_active = TRUE
	`); err != nil {
		t.Fatalf("failed to seed voucher STRICT5: %v", err)
	}

	// 1. ทดสอบเพดานต่ออีเมล (1 อีเมล ยิงพร้อมกัน 15 ครั้ง)
	startUser := make(chan struct{})
	var wgUser sync.WaitGroup
	for i := 0; i < 15; i++ {
		wgUser.Add(1)
		go func(idx int) {
			defer wgUser.Done()
			<-startUser
			idem := fmt.Sprintf("inv4_user_%d_%d", time.Now().UnixNano(), idx)
			oid, tot, st, _ := env.createOrder(idem, "DIAMOND-1000", 1, "spammer@test.com", "STRICT5")
			if st == http.StatusCreated {
				_ = env.sendWebhook(fmt.Sprintf("evt_v_user_%d", idx), "payment.succeeded", oid, tot)
			}
		}(i)
	}
	close(startUser)
	wgUser.Wait()

	// 2. ทดสอบเพดานรวม (30 อีเมลไม่ซ้ำ ยิงพร้อมกัน)
	startGlobal := make(chan struct{})
	var wgGlobal sync.WaitGroup
	for i := 0; i < 30; i++ {
		wgGlobal.Add(1)
		go func(idx int) {
			defer wgGlobal.Done()
			<-startGlobal
			email := fmt.Sprintf("valid_user_%d@test.com", idx)
			idem := fmt.Sprintf("inv4_glob_%d_%d", time.Now().UnixNano(), idx)
			oid, tot, st, _ := env.createOrder(idem, "DIAMOND-1000", 1, email, "STRICT5")
			if st == http.StatusCreated {
				_ = env.sendWebhook(fmt.Sprintf("evt_v_glob_%d", idx), "payment.succeeded", oid, tot)
			}
		}(i)
	}
	close(startGlobal)
	wgGlobal.Wait()

	var usedCount int
	if err := env.db.QueryRow(ctx, `SELECT used_count FROM vouchers WHERE code = 'STRICT5'`).Scan(&usedCount); err != nil {
		t.Fatalf("failed to query voucher used_count: %v", err)
	}
	if usedCount > 5 {
		t.Errorf("global voucher usage (%d) must not exceed usage_limit (5)", usedCount)
	}

	var spammerCount int
	if err := env.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM voucher_usages vu
		JOIN vouchers v ON v.id = vu.voucher_id
		WHERE v.code = 'STRICT5' AND vu.customer_email = 'spammer@test.com' AND vu.is_active = TRUE
	`).Scan(&spammerCount); err != nil {
		t.Fatalf("failed to query voucher_usages: %v", err)
	}
	if spammerCount > 1 {
		t.Errorf("per-email usage (%d) must not exceed limit_per_email (1)", spammerCount)
	}
}

// ============================================================================
// INVARIANT 5: ยอดเงินในระบบต้อง Reconcile กับ Provider ได้ทุกสตางค์
// ============================================================================
func TestInvariant5_ExactSatangReconciliation(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	type settlementRecord struct {
		OrderID   string `json:"order_id"`
		Amount    int64  `json:"amount"`
		Reference string `json:"reference"`
	}
	var providerRecords []settlementRecord
	var expectedMatchedTotal int64

	// 1. สร้างออเดอร์จ่ายสำเร็จตรงยอด 5 รายการ
	for i := 0; i < 5; i++ {
		oid, tot, st, errBody := env.createOrder(fmt.Sprintf("inv5_ok_%d", i), "DIAMOND-1000", 1, fmt.Sprintf("rec_ok_%d@test.com", i), "")
		if st != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", st, errBody)
		}
		if whStatus := env.sendWebhook(fmt.Sprintf("evt_rec_ok_%d", i), "payment.succeeded", oid, tot); whStatus != http.StatusOK {
			t.Fatalf("expected webhook 200 OK, got %d", whStatus)
		}

		providerRecords = append(providerRecords, settlementRecord{
			OrderID:   oid,
			Amount:    tot,
			Reference: fmt.Sprintf("SET-OK-%d", i),
		})
		expectedMatchedTotal += tot
	}

	// 2. สร้างออเดอร์จ่ายยอดไม่ตรง (Mismatch 100 สตางค์) 1 รายการ -> ต้องเข้า NEEDS_REVIEW
	mismatchOID, actualTot, st, errBody := env.createOrder("inv5_mismatch", "DIAMOND-1000", 1, "rec_mismatch@test.com", "")
	if st != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", st, errBody)
	}
	paidShort := actualTot - 100
	if whStatus := env.sendWebhook("evt_rec_mismatch", "payment.succeeded", mismatchOID, paidShort); whStatus != http.StatusOK {
		t.Fatalf("expected webhook 200 OK, got %d", whStatus)
	}
	providerRecords = append(providerRecords, settlementRecord{
		OrderID:   mismatchOID,
		Amount:    paidShort,
		Reference: "SET-MISMATCH-1",
	})

	time.Sleep(1500 * time.Millisecond)

	var mismatchStatus string
	if err := env.db.QueryRow(ctx, `SELECT status FROM orders WHERE id = $1`, mismatchOID).Scan(&mismatchStatus); err != nil {
		t.Fatalf("failed to query mismatch order: %v", err)
	}
	if mismatchStatus != "NEEDS_REVIEW" {
		t.Errorf("amount mismatch order must be flagged NEEDS_REVIEW, got %s", mismatchStatus)
	}

	var systemPaidSum int64
	if err := env.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(total), 0) FROM orders WHERE status IN ('PAID', 'IN_QUEUE', 'IN_PROGRESS', 'COMPLETED')
	`).Scan(&systemPaidSum); err != nil {
		t.Fatalf("failed to sum paid orders: %v", err)
	}
	if systemPaidSum != expectedMatchedTotal {
		t.Errorf("system paid sum (%d) must match provider settled sum (%d) to the exact satang", systemPaidSum, expectedMatchedTotal)
	}

	todayUTC := time.Now().UTC().Format("2006-01-02")
	recPayload, _ := json.Marshal(map[string]interface{}{
		"settlement_date": todayUTC,
		"records":         providerRecords,
	})
	req, _ := http.NewRequest(http.MethodPost, env.baseURL+"/api/v1/admin/reconciliation", bytes.NewReader(recPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.adminToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("reconciliation request failed: %v", err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("reconciliation endpoint failed (status %d): %s", resp.StatusCode, string(bodyBytes))
	}
}
