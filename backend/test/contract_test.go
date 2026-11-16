package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOpenAPIContract(t *testing.T) {
	// 1. ตรวจสอบว่าไฟล์ openapi.yaml อยู่ที่ Root ของโปรเจกต์และเป็น OpenAPI 3.1
	specBytes, err := os.ReadFile("../../openapi.yaml")
	if err != nil {
		t.Fatalf("openapi.yaml must exist at project root (../../openapi.yaml): %v", err)
	}
	specText := string(specBytes)
	if !strings.Contains(specText, "openapi: 3.1") {
		t.Errorf("openapi.yaml must declare OpenAPI 3.1 (e.g., openapi: 3.1.0)")
	}

	requiredPaths := []string{
		"/products:",
		"/products/{slug}:",
		"/orders:",
		"/orders/{id}:",
		"/orders/track:",
		"/webhooks/payment:",
		"/admin/orders:",
		"/admin/orders/{id}/refund:",
		"/admin/reconciliation:",
	}
	for _, p := range requiredPaths {
		if !strings.Contains(specText, p) {
			t.Errorf("openapi.yaml is missing documented path %s", p)
		}
	}

	env := setupTestEnv(t)

	// 2. Contract: GET /api/v1/products -> { "data": [ ProductResponse... ] }
	t.Run("GET /products matches ProductResponse contract", func(t *testing.T) {
		resp, err := http.Get(env.baseURL + "/api/v1/products")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var body struct {
			Data []map[string]interface{} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if len(body.Data) == 0 {
			t.Fatal("expected non-empty product list")
		}

		requiredFields := []string{"id", "sku", "slug", "name_th", "name_en", "price", "created_at", "updated_at"}
		for _, field := range requiredFields {
			if _, ok := body.Data[0][field]; !ok {
				t.Errorf("ProductResponse missing required field %q", field)
			}
		}
	})

	// 3. Contract: GET /api/v1/products/:slug (404) -> ProblemDetail
	t.Run("GET /products/:slug 404 matches ProblemDetail contract", func(t *testing.T) {
		resp, err := http.Get(env.baseURL + "/api/v1/products/non-existent-slug-999")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
		assertProblemDetailContract(t, resp, http.StatusNotFound)
	})

	// 4. Contract: POST /orders (ขาด Idempotency-Key -> 400 ProblemDetail)
	t.Run("POST /orders without Idempotency-Key matches 400 ProblemDetail contract", func(t *testing.T) {
		payload := []byte(`{"customer_email":"test@example.com","game_uid":"12345","payment_method":"PROMPTPAY","items":[{"sku":"DIAMOND-1000","quantity":1}]}`)
		resp, err := http.Post(env.baseURL+"/api/v1/orders", "application/json", bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
		assertProblemDetailContract(t, resp, http.StatusBadRequest)
	})

	// 5. Contract: POST /orders, GET /orders/:id, POST /orders/track
	t.Run("Order lifecycle matches OrderResp & TrackOrderResp contracts", func(t *testing.T) {
		payload := map[string]interface{}{
			"game_uid":       "UID-CONTRACT-01",
			"customer_email": "contract@test.com",
			"payment_method": "PROMPTPAY",
			"items": []map[string]interface{}{
				{"sku": "DIAMOND-1000", "quantity": 1},
			},
		}
		raw, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, env.baseURL+"/api/v1/orders", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", fmt.Sprintf("contract_idem_%d", time.Now().UnixNano()))

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("create order failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", resp.StatusCode)
		}

		var rawMap map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&rawMap); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		orderObj := extractDataObject(rawMap)
		assertOrderRespFields(t, orderObj)

		orderID, _ := orderObj["id"].(string)
		if orderID == "" {
			t.Fatal("missing order id in response")
		}

		// GET /api/v1/orders/:id
		getResp, err := http.Get(env.baseURL + "/api/v1/orders/" + orderID)
		if err != nil {
			t.Fatalf("get order failed: %v", err)
		}
		defer getResp.Body.Close()
		if getResp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", getResp.StatusCode)
		}
		var getMap map[string]interface{}
		_ = json.NewDecoder(getResp.Body).Decode(&getMap)
		assertOrderRespFields(t, extractDataObject(getMap))

		// POST /api/v1/orders/track
		trackRaw, _ := json.Marshal(map[string]string{
			"order_id":       orderID,
			"customer_email": "contract@test.com",
		})
		trackResp, err := http.Post(env.baseURL+"/api/v1/orders/track", "application/json", bytes.NewReader(trackRaw))
		if err != nil {
			t.Fatalf("track order failed: %v", err)
		}
		defer trackResp.Body.Close()
		if trackResp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", trackResp.StatusCode)
		}
		var trackMap map[string]interface{}
		_ = json.NewDecoder(trackResp.Body).Decode(&trackMap)
		trackObj := extractDataObject(trackMap)
		assertOrderRespFields(t, trackObj)
		if _, hasEvents := trackObj["events"]; !hasEvents {
			t.Errorf("TrackOrderResp missing required field 'events'")
		}
	})

	// 6. Contract: Admin Endpoints (GET /admin/orders, GET /admin/audit-logs)
	t.Run("Admin endpoints match contract", func(t *testing.T) {
		adminPaths := []string{"/api/v1/admin/orders", "/api/v1/admin/audit-logs"}
		for _, path := range adminPaths {
			req, _ := http.NewRequest(http.MethodGet, env.baseURL+path, nil)
			req.Header.Set("Authorization", "Bearer "+env.adminToken)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("GET %s failed: %v", path, err)
			}
			var body map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s expected 200, got %d", path, resp.StatusCode)
			}
			if _, ok := body["data"]; !ok {
				t.Errorf("GET %s missing 'data' wrapper field", path)
			}
			if _, ok := body["pagination"]; !ok {
				t.Errorf("GET %s missing 'pagination' metadata field", path)
			}
		}
	})
}

func extractDataObject(m map[string]interface{}) map[string]interface{} {
	if inner, ok := m["data"].(map[string]interface{}); ok {
		return inner
	}
	return m
}

func assertOrderRespFields(t *testing.T, obj map[string]interface{}) {
	t.Helper()
	required := []string{
		"id", "customer_email", "game_uid", "payment_method",
		"subtotal", "discount", "net", "fee", "total", "vat",
		"status", "expires_at", "created_at", "items",
	}
	for _, f := range required {
		if _, ok := obj[f]; !ok {
			t.Errorf("OrderResp missing required field %q", f)
		}
	}
}

func assertProblemDetailContract(t *testing.T, resp *http.Response, expectedStatus int) {
	t.Helper()
	var prob map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&prob); err != nil {
		t.Fatalf("error response is not valid JSON ProblemDetail: %v", err)
	}
	for _, f := range []string{"type", "title", "status", "detail", "code"} {
		if _, ok := prob[f]; !ok {
			t.Errorf("ProblemDetail missing required field %q", f)
		}
	}
	if statusVal, ok := prob["status"].(float64); ok && int(statusVal) != expectedStatus {
		t.Errorf("ProblemDetail.status = %v, want %d", statusVal, expectedStatus)
	}
}
