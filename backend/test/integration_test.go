package test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func getTestConfig(t *testing.T) (baseURL, webhookSecret, jwtSecret string) {
	t.Helper()
	baseURL = os.Getenv("TEST_API_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080/api/v1"
	}
	webhookSecret = os.Getenv("PAYMENT_WEBHOOK_SECRET")
	if webhookSecret == "" {
		webhookSecret = "whsec_inblack_topup_secret_2026"
	}
	jwtSecret = os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "inblack_topup_jwt_super_secret_2026"
	}
	return baseURL, webhookSecret, jwtSecret
}

func resetTestState(t *testing.T, baseURL string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/_test/reset", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("Skipping integration test (API unreachable at %s): %v", baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		t.Skip("Skipping integration test: /_test/reset returned 404 (set APP_ENV=test on API container to run)")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("failed to reset test state: status=%d body=%s", resp.StatusCode, string(body))
	}
}

func mintAdminJWT(t *testing.T, jwtSecret, email, role string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"email": email,
		"role":  role,
		"sub":   email,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(1 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		t.Fatalf("failed to sign test JWT: %v", err)
	}
	return signed
}

func signWebhookPayload(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestIntegration_Idempotency(t *testing.T) {
	baseURL, _, _ := getTestConfig(t)
	resetTestState(t, baseURL)

	idemKey := fmt.Sprintf("idem-test-%d", time.Now().UnixNano())
	payload1 := []byte(`{
		"customer_email": "alice@example.com",
		"game_uid": "100200300",
		"payment_method": "PROMPTPAY",
		"items": [{"sku": "DIAMOND-1000", "quantity": 1}]
	}`)

	req1, _ := http.NewRequest(http.MethodPost, baseURL+"/orders", bytes.NewReader(payload1))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Idempotency-Key", idemKey)
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("request 1 failed: %v", err)
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", resp1.StatusCode)
	}
	var res1 struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp1.Body).Decode(&res1)

	req2, _ := http.NewRequest(http.MethodPost, baseURL+"/orders", bytes.NewReader(payload1))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Idempotency-Key", idemKey)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("request 2 failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created on replay, got %d", resp2.StatusCode)
	}
	var res2 struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp2.Body).Decode(&res2)
	if res1.Data.ID != res2.Data.ID {
		t.Errorf("expected same order ID %s, got %s", res1.Data.ID, res2.Data.ID)
	}

	payloadDiff := []byte(`{
		"customer_email": "alice@example.com",
		"game_uid": "999999999",
		"payment_method": "PROMPTPAY",
		"items": [{"sku": "DIAMOND-1000", "quantity": 1}]
	}`)
	req3, _ := http.NewRequest(http.MethodPost, baseURL+"/orders", bytes.NewReader(payloadDiff))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Idempotency-Key", idemKey)
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatalf("request 3 failed: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 Unprocessable Entity for reused key with different body, got %d", resp3.StatusCode)
	}
}

func TestIntegration_Concurrency_FlashSale(t *testing.T) {
	baseURL, _, _ := getTestConfig(t)
	resetTestState(t, baseURL)

	totalRequests := 120
	var successCount int32
	var conflictCount int32
	var wg sync.WaitGroup

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			body := []byte(fmt.Sprintf(`{
				"customer_email": "buyer-%d@example.com",
				"game_uid": "UID-%d",
				"payment_method": "PROMPTPAY",
				"items": [{"sku": "FLASH-SKIN-01", "quantity": 1}]
			}`, idx, idx))

			req, _ := http.NewRequest(http.MethodPost, baseURL+"/orders", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", fmt.Sprintf("flash-concurrent-%d-%d", time.Now().UnixNano(), idx))

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("worker %d HTTP error: %v", idx, err)
				return
			}
			defer resp.Body.Close()

			switch resp.StatusCode {
			case http.StatusCreated:
				atomic.AddInt32(&successCount, 1)
			case http.StatusConflict:
				atomic.AddInt32(&conflictCount, 1)
			default:
				respBody, _ := io.ReadAll(resp.Body)
				t.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
			}
		}(i)
	}
	wg.Wait()

	if successCount != 100 {
		t.Errorf("expected exactly 100 successful orders (stock=100), got %d", successCount)
	}
	if conflictCount != 20 {
		t.Errorf("expected 20 out-of-stock conflicts, got %d", conflictCount)
	}
}

func TestIntegration_WebhookSignatureAndReplay(t *testing.T) {
	baseURL, webhookSecret, _ := getTestConfig(t)
	resetTestState(t, baseURL)

	// สร้างออเดอร์ทดสอบ
	orderBody := []byte(`{
		"customer_email": "bob@example.com",
		"game_uid": "555666",
		"payment_method": "PROMPTPAY",
		"items": [{"sku": "DIAMOND-1000", "quantity": 1}]
	}`)
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/orders", bytes.NewReader(orderBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprintf("wh-order-%d", time.Now().UnixNano()))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create order failed: %v", err)
	}
	var created struct {
		Data struct {
			ID    string `json:"id"`
			Total int64  `json:"total"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	whPayload := []byte(fmt.Sprintf(`{
		"event_id": "evt_%d",
		"event_type": "payment.succeeded",
		"order_id": "%s",
		"amount": %d,
		"occurred_at": "%s"
	}`, time.Now().UnixNano(), created.Data.ID, created.Data.Total, time.Now().UTC().Format(time.RFC3339)))

	badReq, _ := http.NewRequest(http.MethodPost, baseURL+"/webhooks/payment", bytes.NewReader(whPayload))
	badReq.Header.Set("Content-Type", "application/json")
	badReq.Header.Set("X-Webhook-Signature", "sha256=deadbeef")
	badResp, _ := http.DefaultClient.Do(badReq)
	badResp.Body.Close()
	if badResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for invalid signature, got %d", badResp.StatusCode)
	}

	validSig := signWebhookPayload(webhookSecret, whPayload)
	okReq1, _ := http.NewRequest(http.MethodPost, baseURL+"/webhooks/payment", bytes.NewReader(whPayload))
	okReq1.Header.Set("Content-Type", "application/json")
	okReq1.Header.Set("X-Webhook-Signature", validSig)
	okResp1, err := http.DefaultClient.Do(okReq1)
	if err != nil {
		t.Fatalf("webhook 1 failed: %v", err)
	}
	var whRes1 map[string]interface{}
	_ = json.NewDecoder(okResp1.Body).Decode(&whRes1)
	okResp1.Body.Close()
	if okResp1.StatusCode != http.StatusOK || whRes1["status"] != "processed" {
		t.Errorf("expected status=processed, got code=%d body=%v", okResp1.StatusCode, whRes1)
	}

	okReq2, _ := http.NewRequest(http.MethodPost, baseURL+"/webhooks/payment", bytes.NewReader(whPayload))
	okReq2.Header.Set("Content-Type", "application/json")
	okReq2.Header.Set("X-Webhook-Signature", validSig)
	okResp2, _ := http.DefaultClient.Do(okReq2)
	var whRes2 map[string]interface{}
	_ = json.NewDecoder(okResp2.Body).Decode(&whRes2)
	okResp2.Body.Close()
	if whRes2["status"] != "already_processed" {
		t.Errorf("expected already_processed on duplicate event_id, got %v", whRes2)
	}
}

func TestIntegration_RBAC_And_FailRetryRefund(t *testing.T) {
	baseURL, webhookSecret, jwtSecret := getTestConfig(t)
	resetTestState(t, baseURL)

	viewerToken := mintAdminJWT(t, jwtSecret, "viewer@inblack.gg", "viewer")
	supportToken := mintAdminJWT(t, jwtSecret, "support@inblack.gg", "support")
	financeToken := mintAdminJWT(t, jwtSecret, "finance@inblack.gg", "finance")

	orderBody := []byte(`{
		"customer_email": "failcase@example.com",
		"game_uid": "FAIL-9999",
		"payment_method": "PROMPTPAY",
		"items": [{"sku": "DIAMOND-1000", "quantity": 1}]
	}`)
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/orders", bytes.NewReader(orderBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprintf("fail-e2e-%d", time.Now().UnixNano()))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create fail order failed: %v", err)
	}
	var created struct {
		Data struct {
			ID    string `json:"id"`
			Total int64  `json:"total"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	whPayload := []byte(fmt.Sprintf(`{
		"event_id": "evt_fail_%d",
		"event_type": "payment.succeeded",
		"order_id": "%s",
		"amount": %d,
		"occurred_at": "%s"
	}`, time.Now().UnixNano(), created.Data.ID, created.Data.Total, time.Now().UTC().Format(time.RFC3339)))
	whReq, _ := http.NewRequest(http.MethodPost, baseURL+"/webhooks/payment", bytes.NewReader(whPayload))
	whReq.Header.Set("Content-Type", "application/json")
	whReq.Header.Set("X-Webhook-Signature", signWebhookPayload(webhookSecret, whPayload))
	whResp, _ := http.DefaultClient.Do(whReq)
	whResp.Body.Close()

	var currentStatus string
	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		getResp, err := http.Get(baseURL + "/orders/" + created.Data.ID)
		if err == nil {
			var ord struct {
				Data struct {
					Status string `json:"status"`
				} `json:"data"`
			}
			_ = json.NewDecoder(getResp.Body).Decode(&ord)
			getResp.Body.Close()
			currentStatus = ord.Data.Status
			if currentStatus == "FAILED" {
				break
			}
		}
	}
	if currentStatus != "FAILED" {
		t.Fatalf("expected worker to mark FAIL-9999 order as FAILED, got %s", currentStatus)
	}

	refundBody := []byte(`{"reason": "Customer requested refund"}`)
	viewerRefundReq, _ := http.NewRequest(http.MethodPost, baseURL+"/admin/orders/"+created.Data.ID+"/refund", bytes.NewReader(refundBody))
	viewerRefundReq.Header.Set("Authorization", "Bearer "+viewerToken)
	viewerRefundReq.Header.Set("Content-Type", "application/json")
	viewerRefundReq.Header.Set("Idempotency-Key", fmt.Sprintf("ref-viewer-%d", time.Now().UnixNano()))
	viewerRefundResp, _ := http.DefaultClient.Do(viewerRefundReq)
	viewerRefundResp.Body.Close()
	if viewerRefundResp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for viewer refund attempt, got %d", viewerRefundResp.StatusCode)
	}

	retryReq, _ := http.NewRequest(http.MethodPost, baseURL+"/admin/orders/"+created.Data.ID+"/retry-fulfillment", nil)
	retryReq.Header.Set("Authorization", "Bearer "+supportToken)
	retryResp, _ := http.DefaultClient.Do(retryReq)
	retryResp.Body.Close()
	if retryResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for support retry, got %d", retryResp.StatusCode)
	}

	time.Sleep(1500 * time.Millisecond)

	finRefundReq, _ := http.NewRequest(http.MethodPost, baseURL+"/admin/orders/"+created.Data.ID+"/refund", bytes.NewReader(refundBody))
	finRefundReq.Header.Set("Authorization", "Bearer "+financeToken)
	finRefundReq.Header.Set("Content-Type", "application/json")
	finRefundReq.Header.Set("Idempotency-Key", fmt.Sprintf("ref-fin-%d", time.Now().UnixNano()))
	finRefundResp, _ := http.DefaultClient.Do(finRefundReq)
	var refData struct {
		Data struct {
			OrderStatus string `json:"order_status"`
		} `json:"data"`
	}
	_ = json.NewDecoder(finRefundResp.Body).Decode(&refData)
	finRefundResp.Body.Close()
	if finRefundResp.StatusCode != http.StatusOK || refData.Data.OrderStatus != "REFUNDED" {
		t.Errorf("expected 200 OK and REFUNDED status, got code=%d status=%s", finRefundResp.StatusCode, refData.Data.OrderStatus)
	}
}

func TestIntegration_VoucherAndProductLimits(t *testing.T) {
	baseURL, _, _ := getTestConfig(t)
	resetTestState(t, baseURL)

	// 1. ทดสอบยอดไม่ถึง min_subtotal ของ SAVE10 (ขั้นต่ำ 200.00 บาท = 20000 สตางค์)
	lowBody := []byte(`{
		"customer_email": "voucher-user@example.com",
		"game_uid": "111222",
		"payment_method": "PROMPTPAY",
		"voucher_code": "SAVE10",
		"items": [{"sku": "FLASH-SKIN-01", "quantity": 1}]
	}`)
	req1, _ := http.NewRequest(http.MethodPost, baseURL+"/orders", bytes.NewReader(lowBody))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Idempotency-Key", fmt.Sprintf("v-min-%d", time.Now().UnixNano()))
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 when subtotal < voucher min_subtotal, got %d", resp1.StatusCode)
	}

	// 2. สั่งซื้อ FLASH-SKIN-01 ครั้งที่ 1 (สำเร็จ 201)
	validBody := []byte(`{
		"customer_email": "limit-user@example.com",
		"game_uid": "333444",
		"payment_method": "PROMPTPAY",
		"items": [{"sku": "FLASH-SKIN-01", "quantity": 1}]
	}`)
	req2, _ := http.NewRequest(http.MethodPost, baseURL+"/orders", bytes.NewReader(validBody))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Idempotency-Key", fmt.Sprintf("lim-1-%d", time.Now().UnixNano()))
	resp2, _ := http.DefaultClient.Do(req2)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 on first purchase, got %d", resp2.StatusCode)
	}

	// 3. อีเมลเดิมสั่งซื้อ FLASH-SKIN-01 ครั้งที่ 2 -> ต้องติด 409 Conflict (limit_per_email)
	req3, _ := http.NewRequest(http.MethodPost, baseURL+"/orders", bytes.NewReader(validBody))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Idempotency-Key", fmt.Sprintf("lim-2-%d", time.Now().UnixNano()))
	resp3, _ := http.DefaultClient.Do(req3)
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusConflict {
		t.Errorf("expected 409 Conflict on exceeding per-email product limit, got %d", resp3.StatusCode)
	}
}

func TestIntegration_WebhookAmountMismatch_NeedsReview(t *testing.T) {
	baseURL, webhookSecret, _ := getTestConfig(t)
	resetTestState(t, baseURL)

	orderBody := []byte(`{
		"customer_email": "mismatch@example.com",
		"game_uid": "777888",
		"payment_method": "PROMPTPAY",
		"items": [{"sku": "DIAMOND-1000", "quantity": 1}]
	}`)
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/orders", bytes.NewReader(orderBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprintf("mm-ord-%d", time.Now().UnixNano()))
	resp, _ := http.DefaultClient.Do(req)
	var created struct {
		Data struct {
			ID    string `json:"id"`
			Total int64  `json:"total"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	// ยิง Webhook ด้วยยอดเงินไม่ตรง (Total - 100)
	whPayload := []byte(fmt.Sprintf(`{
		"event_id": "evt_mm_%d",
		"event_type": "payment.succeeded",
		"order_id": "%s",
		"amount": %d,
		"occurred_at": "%s"
	}`, time.Now().UnixNano(), created.Data.ID, created.Data.Total-100, time.Now().UTC().Format(time.RFC3339)))

	whReq, _ := http.NewRequest(http.MethodPost, baseURL+"/webhooks/payment", bytes.NewReader(whPayload))
	whReq.Header.Set("Content-Type", "application/json")
	whReq.Header.Set("X-Webhook-Signature", signWebhookPayload(webhookSecret, whPayload))
	whResp, _ := http.DefaultClient.Do(whReq)
	whResp.Body.Close()

	// ตรวจสอบว่าสถานะกลายเป็น NEEDS_REVIEW
	getResp, _ := http.Get(baseURL + "/orders/" + created.Data.ID)
	var ord struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	_ = json.NewDecoder(getResp.Body).Decode(&ord)
	getResp.Body.Close()

	if ord.Data.Status != "NEEDS_REVIEW" {
		t.Errorf("expected NEEDS_REVIEW on amount mismatch, got %s", ord.Data.Status)
	}
}
