package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

type WebhookPayload struct {
	EventID    string    `json:"event_id"`
	EventType  string    `json:"event_type"`
	OrderID    string    `json:"order_id"`
	Amount     int64     `json:"amount"`
	OccurredAt time.Time `json:"occurred_at"`
}

type TriggerReq struct {
	OrderID   string `json:"order_id"`
	Amount    int64  `json:"amount"`
	Scenario  string `json:"scenario"`
	ReturnURL string `json:"return_url"`
}

var (
	port          = getEnv("PORT", "9000")
	webhookURL    = getEnv("TARGET_WEBHOOK_URL", "http://proxy:8080/api/v1/webhooks/payment")
	webhookSecret = getEnv("PAYMENT_WEBHOOK_SECRET", "")
)

func main() {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"mock-pay"}`))
	})

	http.HandleFunc("/checkout", handleCheckoutPage)
	http.HandleFunc("/api/simulate", handleSimulatePayment)

	log.Printf("[mock-pay] Listening on :%s -> Target Webhook: %s", port, webhookURL)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleCheckoutPage(w http.ResponseWriter, r *http.Request) {
	orderID := r.URL.Query().Get("order_id")
	amountStr := r.URL.Query().Get("amount")
	returnURL := r.URL.Query().Get("return_url")
	if returnURL == "" && orderID != "" {
		returnURL = "http://localhost:3000/orders/" + orderID
	}
	amount, _ := strconv.ParseInt(amountStr, 10, 64)

	data := map[string]interface{}{
		"OrderID":   orderID,
		"Amount":    amount,
		"AmountTHB": fmt.Sprintf("%.2f", float64(amount)/100.0),
		"ReturnURL": returnURL,
	}

	tmpl := template.Must(template.New("checkout").Parse(checkoutHTML))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, data)
}

func handleSimulatePayment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req TriggerReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	eventID := "evt_" + randomHex(12)

	switch req.Scenario {
	case "success":
		go deliverWithRetry(WebhookPayload{
			EventID:    eventID,
			EventType:  "payment.succeeded",
			OrderID:    req.OrderID,
			Amount:     req.Amount,
			OccurredAt: time.Now().UTC(),
		})

	case "fail":
		go deliverWithRetry(WebhookPayload{
			EventID:    eventID,
			EventType:  "payment.failed",
			OrderID:    req.OrderID,
			Amount:     req.Amount,
			OccurredAt: time.Now().UTC(),
		})

	case "duplicate_5":
		payload := WebhookPayload{
			EventID:    eventID,
			EventType:  "payment.succeeded",
			OrderID:    req.OrderID,
			Amount:     req.Amount,
			OccurredAt: time.Now().UTC(),
		}
		for i := 1; i <= 5; i++ {
			go func(attempt int) {
				log.Printf("[mock-pay] Sending duplicate webhook #%d for event_id=%s", attempt, eventID)
				deliverWithRetry(payload)
			}(i)
		}

	case "delay_60s":
		go func() {
			log.Printf("[mock-pay] Delaying webhook 60s for order=%s...", req.OrderID)
			time.Sleep(60 * time.Second)
			deliverWithRetry(WebhookPayload{
				EventID:    eventID,
				EventType:  "payment.succeeded",
				OrderID:    req.OrderID,
				Amount:     req.Amount,
				OccurredAt: time.Now().UTC(),
			})
		}()

	case "mismatch":
		wrongAmount := req.Amount - 100
		if wrongAmount <= 0 {
			wrongAmount = req.Amount + 100
		}
		go deliverWithRetry(WebhookPayload{
			EventID:    eventID,
			EventType:  "payment.succeeded",
			OrderID:    req.OrderID,
			Amount:     wrongAmount,
			OccurredAt: time.Now().UTC(),
		})

	default:
		http.Error(w, "Unknown scenario", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "scheduled",
		"event_id": eventID,
		"scenario": req.Scenario,
	})
}

func deliverWithRetry(payload WebhookPayload) {
	rawBody, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[mock-pay] Marshal error: %v", err)
		return
	}

	maxRetries := 8
	backoff := 1 * time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("[mock-pay] Retry #%d/%d for event=%s after %v", attempt, maxRetries, payload.EventID, backoff)
			time.Sleep(backoff)
			backoff *= 2
		}

		sig := computeWebhookSignature(webhookSecret, rawBody)

		req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(rawBody))
		if err != nil {
			log.Printf("[mock-pay] Request build error: %v", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Signature", "sha256="+sig)

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[mock-pay] HTTP error (attempt %d): %v", attempt, err)
			continue
		}
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			log.Printf("[mock-pay] Delivered event=%s order=%s status=%d", payload.EventID, payload.OrderID, resp.StatusCode)
			return
		}
		log.Printf("[mock-pay] Non-2xx status=%d for event=%s (attempt %d)", resp.StatusCode, payload.EventID, attempt)
	}

	log.Printf("[mock-pay] Exhausted all %d retries for event=%s", maxRetries, payload.EventID)
}

func computeWebhookSignature(secret string, rawBody []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	return hex.EncodeToString(mac.Sum(nil))
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

const checkoutHTML = `<!DOCTYPE html>
<html lang="th">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>MockPay Gateway — InBlack Top-Up</title>
  <style>
    body { font-family: system-ui, -apple-system, sans-serif; background: #09090b; color: #f4f4f5; display: flex; justify-content: center; align-items: center; min-height: 100vh; margin: 0; padding: 16px; }
    .card { background: #18181b; border: 1px solid #27272a; border-radius: 16px; padding: 28px; max-width: 460px; width: 100%; box-shadow: 0 20px 40px rgba(0,0,0,0.5); }
    .badge { display: inline-block; background: #dc262622; color: #f87171; border: 1px solid #dc262644; padding: 4px 10px; border-radius: 999px; font-size: 12px; font-weight: 700; margin-bottom: 12px; }
    h1 { margin: 0 0 6px; font-size: 22px; }
    .meta { background: #09090b; border: 1px solid #27272a; border-radius: 12px; padding: 14px; margin: 18px 0; font-size: 14px; }
    .meta div { display: flex; justify-content: space-between; margin-bottom: 6px; }
    .meta div:last-child { margin-bottom: 0; }
    .mono { font-family: ui-monospace, monospace; color: #e4e4e7; }
    .btn { width: 100%; padding: 12px 16px; border: none; border-radius: 10px; font-size: 14px; font-weight: 700; cursor: pointer; margin-bottom: 10px; transition: opacity .15s; text-align: left; }
    .btn:hover { opacity: 0.9; }
    .btn:disabled { opacity: 0.5; cursor: not-allowed; }
    .btn-success { background: #16a34a; color: #fff; }
    .btn-fail { background: #dc2626; color: #fff; }
    .btn-warn { background: #27272a; color: #f4f4f5; border: 1px solid #3f3f46; }
    #status { margin-top: 14px; font-size: 13px; color: #a1a1aa; text-align: center; }
  </style>
</head>
<body>
  <div class="card">
    <span class="badge">MOCK-PAY GATEWAY</span>
    <h1>ชำระเงินคำสั่งซื้อ</h1>
    <p style="margin:0;color:#a1a1aa;font-size:13px;">เลือกสถานการณ์จำลองการยิง Webhook กลับไปยัง InBlack Top-Up</p>

    <div class="meta">
      <div><span style="color:#a1a1aa;">Order ID</span><span class="mono">{{.OrderID}}</span></div>
      <div><span style="color:#a1a1aa;">ยอดชำระสุทธิ</span><span class="mono" style="color:#4ade80;font-weight:700;">฿{{.AmountTHB}} ({{.Amount}} สตางค์)</span></div>
    </div>

    <button class="btn btn-success" onclick="trigger('success')">1. จ่ายสำเร็จ (payment.succeeded)</button>
    <button class="btn btn-fail" onclick="trigger('fail')">2. จ่ายไม่สำเร็จ (payment.failed)</button>
    <button class="btn btn-warn" onclick="trigger('duplicate_5')">3. จ่ายสำเร็จ + ส่ง Webhook ซ้ำ 5 ครั้ง</button>
    <button class="btn btn-warn" onclick="trigger('delay_60s')">4. จ่ายสำเร็จ + ส่ง Webhook ช้า 60 วินาที</button>
    <button class="btn btn-warn" onclick="trigger('mismatch')">5. จ่ายยอดไม่ตรง (Amount Mismatch → NEEDS_REVIEW)</button>

    <div id="status"></div>
  </div>

  <script>
    const orderID = "{{.OrderID}}";
    const amount = {{.Amount}};
    const returnURL = "{{.ReturnURL}}";

    async function trigger(scenario) {
      if (!orderID) {
        alert("ไม่พบ order_id ใน URL");
        return;
      }
      document.querySelectorAll('.btn').forEach(b => b.disabled = true);
      const statusEl = document.getElementById('status');
      statusEl.textContent = "กำลังจำลองการชำระเงินและส่ง Webhook...";

      try {
        const res = await fetch('/api/simulate', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ order_id: orderID, amount: amount, scenario: scenario, return_url: returnURL })
        });
        const data = await res.json();
        statusEl.textContent = scenario === 'delay_60s'
          ? "ตั้งเวลาส่ง Webhook ในอีก 60 วินาทีแล้ว กำลังพากลับหน้าคำสั่งซื้อ..."
          : "ส่ง Webhook สำเร็จ (" + data.event_id + ") กำลังพากลับหน้าคำสั่งซื้อ...";

        setTimeout(() => {
          if (returnURL) window.location.href = returnURL;
        }, 1200);
      } catch (err) {
        statusEl.textContent = "เกิดข้อผิดพลาด: " + err.message;
        document.querySelectorAll('.btn').forEach(b => b.disabled = false);
      }
    }
  </script>
</body>
</html>`
