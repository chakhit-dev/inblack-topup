package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/smtp"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/inblack/topup-backend/internal/database"
	"github.com/inblack/topup-backend/internal/domain"
	"github.com/inblack/topup-backend/internal/handler"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	consumerGroup = "fulfillment-workers"
)

func main() {
	instanceID := getEnv("INSTANCE_ID")
	databaseURL := getEnv("DATABASE_URL")
	redisURL := getEnv("REDIS_URL")
	smtpHost := getEnv("SMTP_HOST")
	smtpPort := getEnv("SMTP_PORT")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initCtx, initCancel := context.WithTimeout(ctx, 15*time.Second)
	dbPool, err := database.NewPostgresPool(initCtx, databaseURL)
	if err != nil {
		log.Fatalf("[%s] Failed to connect Postgres: %v", instanceID, err)
	}
	defer dbPool.Close()

	redisClient, err := database.NewRedisClient(initCtx, redisURL)
	if err != nil {
		log.Fatalf("[%s] Failed to connect Redis: %v", instanceID, err)
	}
	defer redisClient.Close()
	initCancel()

	// สร้าง Consumer Group หากยังไม่มี (เริ่มอ่านตั้งแต่ข้อความแรก "0")
	err = redisClient.XGroupCreateMkStream(ctx, handler.FulfillmentStreamKey, consumerGroup, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		log.Fatalf("[%s] Failed to create consumer group: %v", instanceID, err)
	}

	log.Printf("[%s] Fulfillment Worker & Sweeper started...", instanceID)

	// รัน Background Sweeper แยก Goroutine (เช็กออเดอร์หมดอายุ & ค้างทุก 10 วินาที)
	go runSweeper(ctx, dbPool, redisClient, instanceID)

	// รัน Fulfillment Stream Consumer Loop
	go runFulfillmentConsumer(ctx, dbPool, redisClient, instanceID, smtpHost, smtpPort)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Printf("[%s] Shutting down worker gracefully...", instanceID)
	cancel()
	time.Sleep(1 * time.Second)
}

func runFulfillmentConsumer(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client, consumerName, smtpHost, smtpPort string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		streams, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    consumerGroup,
			Consumer: consumerName,
			Streams:  []string{handler.FulfillmentStreamKey, ">"},
			Count:    5,
			Block:    2 * time.Second,
		}).Result()

		if err != nil {
			if errors.Is(err, redis.Nil) || errors.Is(err, context.Canceled) {
				continue
			}
			log.Printf("[%s] XReadGroup error: %v", consumerName, err)
			time.Sleep(1 * time.Second)
			continue
		}

		for _, stream := range streams {
			for _, msg := range stream.Messages {
				orderID, ok := msg.Values["order_id"].(string)
				if !ok || orderID == "" {
					_ = rdb.XAck(ctx, handler.FulfillmentStreamKey, consumerGroup, msg.ID).Err()
					continue
				}

				if err := processFulfillment(ctx, db, consumerName, orderID, smtpHost, smtpPort); err != nil {
					log.Printf("[%s] Failed processing order %s: %v", consumerName, orderID, err)
				} else {
					_ = rdb.XAck(ctx, handler.FulfillmentStreamKey, consumerGroup, msg.ID).Err()
				}
			}
		}
	}
}

func processFulfillment(ctx context.Context, db *pgxpool.Pool, workerID, orderID, smtpHost, smtpPort string) error {
	// ล็อกออเดอร์และเปลี่ยนสถานะ IN_QUEUE -> IN_PROGRESS
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var currentStatus, customerEmail, gameUID string
	var total int64
	err = tx.QueryRow(ctx, `
		SELECT status, customer_email, game_uid, total
		FROM orders
		WHERE id = $1
		FOR UPDATE
	`, orderID).Scan(&currentStatus, &customerEmail, &gameUID, &total)
	if err != nil {
		return err
	}

	if domain.OrderStatus(currentStatus) != domain.StatusInQueue {
		// ถูกประมวลผลไปแล้วหรือไม่ได้อยู่ในคิว ข้ามได้เลย
		return nil
	}

	if _, err := tx.Exec(ctx, `
		UPDATE orders
		SET status = 'IN_PROGRESS', processing_started_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, orderID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO order_events (order_id, from_status, to_status, actor, reason)
		VALUES ($1, 'IN_QUEUE', 'IN_PROGRESS', $2, 'Worker picked up order for top-up fulfillment')
	`, orderID, workerID); err != nil {
		return err
	}

	// บันทึกหรืออัปเดตประวัติความพยายามเติมเงิน (Idempotent reference ต่อออเดอร์)
	refCode := fmt.Sprintf("FUL-%s", orderID)
	_, err = tx.Exec(ctx, `
		INSERT INTO fulfillment_attempts (order_id, reference, attempt_number, status)
		VALUES ($1, $2, 1, 'PROCESSING')
		ON CONFLICT (order_id) DO UPDATE
		SET attempt_number = fulfillment_attempts.attempt_number + 1,
		    status = 'PROCESSING',
		    updated_at = NOW()
	`, orderID, refCode)
	if err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// จำลองการเชื่อมต่อ Game Publisher API เพื่อเติมเพชร/ไอเทม
	log.Printf("[%s] Fulfilling order %s for Game UID: %s ...", workerID, orderID, gameUID)
	time.Sleep(800 * time.Millisecond)

	// ถ้า game_uid ขึ้นต้นด้วย "FAIL-" ให้จำลองว่าเซิร์ฟเวอร์เกมปฏิเสธ
	isFailed := strings.HasPrefix(strings.ToUpper(gameUID), "FAIL-")

	// บันทึกผลลัพธ์สุดท้าย (COMPLETED หรือ FAILED)
	finTx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer finTx.Rollback(ctx)

	if isFailed {
		errMsg := "Game publisher rejected UID or server timeout"
		_, _ = finTx.Exec(ctx, `
			UPDATE orders SET status = 'FAILED', processing_started_at = NULL, updated_at = NOW() WHERE id = $1
		`, orderID)
		_, _ = finTx.Exec(ctx, `
			UPDATE fulfillment_attempts SET status = 'FAILED', error_message = $1, updated_at = NOW() WHERE order_id = $2
		`, errMsg, orderID)
		_, _ = finTx.Exec(ctx, `
			INSERT INTO order_events (order_id, from_status, to_status, actor, reason)
			VALUES ($1, 'IN_PROGRESS', 'FAILED', $2, $3)
		`, orderID, workerID, errMsg)
		return finTx.Commit(ctx)
	}

	providerResp, _ := json.Marshal(map[string]interface{}{
		"provider":  "InBlackGameDirect",
		"reference": refCode,
		"game_uid":  gameUID,
		"delivered": true,
	})

	if _, err := finTx.Exec(ctx, `
		UPDATE orders SET status = 'COMPLETED', processing_started_at = NULL, updated_at = NOW() WHERE id = $1
	`, orderID); err != nil {
		return err
	}
	if _, err := finTx.Exec(ctx, `
		UPDATE fulfillment_attempts
		SET status = 'SUCCEEDED', provider_response = $1::jsonb, error_message = NULL, updated_at = NOW()
		WHERE order_id = $2
	`, string(providerResp), orderID); err != nil {
		return err
	}
	if _, err := finTx.Exec(ctx, `
		INSERT INTO order_events (order_id, from_status, to_status, actor, reason)
		VALUES ($1, 'IN_PROGRESS', 'COMPLETED', $2, 'Top-up items delivered to game account successfully')
	`, orderID, workerID); err != nil {
		return err
	}

	if err := finTx.Commit(ctx); err != nil {
		return err
	}

	log.Printf("[%s] Order %s COMPLETED!", workerID, orderID)

	// ส่งใบเสร็จทางอีเมล (Mailpit)
	sendReceiptEmail(smtpHost, smtpPort, customerEmail, orderID, gameUID, total)
	return nil
}

func runSweeper(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client, workerID string) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepExpiredOrders(ctx, db, workerID)
			recoverStuckOrders(ctx, db, rdb, workerID)
		}
	}
}

func sweepExpiredOrders(ctx context.Context, db *pgxpool.Pool, workerID string) {
	rows, err := db.Query(ctx, `
		SELECT id FROM orders
		WHERE status = 'PENDING_PAYMENT' AND expires_at < NOW()
		LIMIT 20
	`)
	if err != nil {
		return
	}
	var expiredIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			expiredIDs = append(expiredIDs, id)
		}
	}
	rows.Close()

	for _, id := range expiredIDs {
		tx, err := db.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			continue
		}

		var st string
		if err := tx.QueryRow(ctx, `SELECT status FROM orders WHERE id = $1 FOR UPDATE`, id).Scan(&st); err != nil || st != "PENDING_PAYMENT" {
			_ = tx.Rollback(ctx)
			continue
		}

		_, _ = tx.Exec(ctx, `UPDATE orders SET status = 'EXPIRED', updated_at = NOW() WHERE id = $1`, id)
		_, _ = tx.Exec(ctx, `
			INSERT INTO order_events (order_id, from_status, to_status, actor, reason)
			VALUES ($1, 'PENDING_PAYMENT', 'EXPIRED', $2, 'Order payment window expired (15m)')
		`, id, workerID)

		// คืนสต็อกสินค้าที่มีจำกัด
		_, _ = tx.Exec(ctx, `
			UPDATE products p
			SET stock = p.stock + oi.quantity, updated_at = NOW()
			FROM order_items oi
			WHERE oi.order_id = $1 AND oi.product_id = p.id AND p.stock IS NOT NULL
		`, id)

		// คืนสิทธิ์ Voucher
		var voucherID string
		if err := tx.QueryRow(ctx, `
			UPDATE voucher_usages SET is_active = FALSE
			WHERE order_id = $1 AND is_active = TRUE
			RETURNING voucher_id
		`, id).Scan(&voucherID); err == nil && voucherID != "" {
			_, _ = tx.Exec(ctx, `
				UPDATE vouchers SET used_count = GREATEST(used_count - 1, 0), updated_at = NOW()
				WHERE id = $1
			`, voucherID)
		}

		if err := tx.Commit(ctx); err == nil {
			log.Printf("[%s] Expired order %s and released stock/voucher", workerID, id)
		}
	}
}

func recoverStuckOrders(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client, workerID string) {
	rows, err := db.Query(ctx, `
		SELECT id FROM orders
		WHERE status = 'IN_PROGRESS' AND processing_started_at < NOW() - INTERVAL '2 minutes'
		LIMIT 10
	`)
	if err != nil {
		return
	}
	var stuckIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			stuckIDs = append(stuckIDs, id)
		}
	}
	rows.Close()

	for _, id := range stuckIDs {
		tx, err := db.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			continue
		}
		var st string
		if err := tx.QueryRow(ctx, `SELECT status FROM orders WHERE id = $1 FOR UPDATE`, id).Scan(&st); err != nil || st != "IN_PROGRESS" {
			_ = tx.Rollback(ctx)
			continue
		}
		_, _ = tx.Exec(ctx, `UPDATE orders SET status = 'IN_QUEUE', processing_started_at = NULL, updated_at = NOW() WHERE id = $1`, id)
		_, _ = tx.Exec(ctx, `
			INSERT INTO order_events (order_id, from_status, to_status, actor, reason)
			VALUES ($1, 'IN_PROGRESS', 'IN_QUEUE', $2, 'Recovered stuck fulfillment job back to queue')
		`, id, workerID)
		if err := tx.Commit(ctx); err == nil {
			_ = rdb.XAdd(ctx, &redis.XAddArgs{
				Stream: handler.FulfillmentStreamKey,
				Values: map[string]interface{}{"order_id": id, "recovered": "true"},
			}).Err()
			log.Printf("[%s] Recovered stuck order %s back to IN_QUEUE", workerID, id)
		}
	}
}

func sendReceiptEmail(host, port, toEmail, orderID, gameUID string, totalSatang int64) {
	addr := fmt.Sprintf("%s:%s", host, port)
	subject := fmt.Sprintf("Subject: [InBlack Top-Up] Receipt for Order %s\r\n", orderID)
	headers := "From: no-reply@inblack.gg\r\nTo: " + toEmail + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=\"utf-8\"\r\n\r\n"
	body := fmt.Sprintf(
		"Thank you for topping up with InBlack!\n\nOrder ID: %s\nGame UID: %s\nTotal Paid: %.2f THB\nStatus: COMPLETED\n",
		orderID, gameUID, float64(totalSatang)/100.0,
	)
	msg := []byte(subject + headers + body)
	if err := smtp.SendMail(addr, nil, "no-reply@inblack.gg", []string{toEmail}, msg); err != nil {
		log.Printf("Failed to send receipt email to %s: %v", toEmail, err)
	}
}

func getEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		log.Fatalf("FATAL: required environment variable %s is not set", key)
	}
	return val
}
