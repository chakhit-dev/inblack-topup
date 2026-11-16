package main

import (
	"encoding/json"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type TopupRequest struct {
	Reference string `json:"reference"`
	GameUID   string `json:"game_uid"`
	SKU       string `json:"sku"`
	Qty       int    `json:"qty"`
}

type TopupRecord struct {
	Reference string `json:"reference"`
	GameUID   string `json:"game_uid"`
	SKU       string `json:"sku"`
	Qty       int    `json:"qty"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type MockGameServer struct {
	mu                    sync.RWMutex
	topups                map[string][]TopupRecord // เก็บประวัติการเติมตาม reference
	successCounts         map[string]int           // นับจำนวนครั้งที่เติมสำเร็จต่อ reference
	failRate              float64
	timeoutRate           float64
	successButTimeoutRate float64
	latencyMs             int
}

func getEnvFloat(key string, fallback float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}

func getEnvInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (s *MockGameServer) handleCreateTopup(w http.ResponseWriter, r *http.Request) {
	var req TopupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json payload"})
		return
	}
	if strings.TrimSpace(req.Reference) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reference is required"})
		return
	}
	if req.Qty <= 0 {
		req.Qty = 1
	}

	// LATENCY_MS: หน่วงทุก request
	if s.latencyMs > 0 {
		time.Sleep(time.Duration(s.latencyMs) * time.Millisecond)
	}

	roll := rand.Float64()

	// 1. TIMEOUT_RATE: ค้างจน client timeout และ ไม่ได้เติม
	if roll < s.timeoutRate {
		select {
		case <-time.After(65 * time.Second):
		case <-r.Context().Done():
		}
		return
	}

	// 2. FAIL_RATE: ตอบ 5xx และ ไม่ได้เติม
	if roll < s.timeoutRate+s.failRate {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "upstream game server failed",
		})
		return
	}

	// 3. บันทึกการเติมสำเร็จ
	// สำคัญ: ตามสเปกข้อ 7 ไม่ทำ idempotency ให้ — ถ้ายิง reference เดิมซ้ำ จะนับเพิ่มและเติมซ้ำ
	record := TopupRecord{
		Reference: req.Reference,
		GameUID:   req.GameUID,
		SKU:       req.SKU,
		Qty:       req.Qty,
		Status:    "SUCCESS",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	s.mu.Lock()
	s.topups[req.Reference] = append(s.topups[req.Reference], record)
	s.successCounts[req.Reference]++
	s.mu.Unlock()

	// 4. SUCCESS_BUT_TIMEOUT_RATE: เติมสำเร็จแล้ว แต่ค้างจน client timeout
	if roll < s.timeoutRate+s.failRate+s.successButTimeoutRate {
		select {
		case <-time.After(65 * time.Second):
		case <-r.Context().Done():
		}
		return
	}

	writeJSON(w, http.StatusOK, record)
}

func (s *MockGameServer) handleGetTopup(w http.ResponseWriter, r *http.Request) {
	ref := strings.TrimPrefix(r.URL.Path, "/topups/")
	if ref == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reference is required"})
		return
	}

	if s.latencyMs > 0 {
		time.Sleep(time.Duration(s.latencyMs) * time.Millisecond)
	}

	s.mu.RLock()
	records, ok := s.topups[ref]
	s.mu.RUnlock()

	if !ok || len(records) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "topup reference not found"})
		return
	}

	// คืนสถานะการเติมล่าสุดของ reference นั้น (ใช้สำหรับ reconcile)
	writeJSON(w, http.StatusOK, records[len(records)-1])
}

func (s *MockGameServer) handleStats(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	countsCopy := make(map[string]int, len(s.successCounts))
	totalSuccess := 0
	for ref, count := range s.successCounts {
		countsCopy[ref] = count
		totalSuccess += count
	}
	s.mu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"counts":           countsCopy,
		"total_references": len(countsCopy),
		"total_success":    totalSuccess,
	})
}

func main() {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "9001"
	}

	srv := &MockGameServer{
		topups:                make(map[string][]TopupRecord),
		successCounts:         make(map[string]int),
		failRate:              getEnvFloat("FAIL_RATE", 0),
		timeoutRate:           getEnvFloat("TIMEOUT_RATE", 0),
		successButTimeoutRate: getEnvFloat("SUCCESS_BUT_TIMEOUT_RATE", 0),
		latencyMs:             getEnvInt("LATENCY_MS", 0),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /topups", srv.handleCreateTopup)
	mux.HandleFunc("GET /topups/{reference}", srv.handleGetTopup)
	mux.HandleFunc("GET /stats", srv.handleStats)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	log.Printf("mock-game listening on :%s (FAIL_RATE=%.2f, TIMEOUT_RATE=%.2f, SUCCESS_BUT_TIMEOUT_RATE=%.2f, LATENCY_MS=%d)",
		port, srv.failRate, srv.timeoutRate, srv.successButTimeoutRate, srv.latencyMs)

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
