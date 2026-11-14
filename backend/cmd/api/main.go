package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/inblack/topup-backend/internal/database"
	"github.com/inblack/topup-backend/internal/handler"
)

func main() {
	port := getEnv("PORT", "8080")
	appEnv := getEnv("APP_ENV", "development")
	instanceID := getEnv("INSTANCE_ID", "api-local")
	databaseURL := getEnv("DATABASE_URL", "")
	redisURL := getEnv("REDIS_URL", "")

	jwtSecret := getEnv("JWT_SECRET", "REDACTED")
	smtpHost := getEnv("SMTP_HOST", "mailpit")
	smtpPort := getEnv("SMTP_PORT", "1025")

	if appEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	initCtx, initCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer initCancel()

	dbPool, err := database.NewPostgresPool(initCtx, databaseURL)
	if err != nil {
		log.Fatalf("[%s] Failed to connect Postgres: %v", instanceID, err)
	}
	defer dbPool.Close()

	if err := database.RunMigrationAndSeed(initCtx, dbPool, "migrations/000001_init_schema.up.sql"); err != nil {
		log.Fatalf("[%s] Failed migration/seed: %v", instanceID, err)
	}

	redisClient, err := database.NewRedisClient(initCtx, redisURL)
	if err != nil {
		log.Fatalf("[%s] Failed to connect Redis: %v", instanceID, err)
	}
	defer redisClient.Close()

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	_ = r.SetTrustedProxies([]string{"172.16.0.0/12", "10.0.0.0/8", "192.168.0.0/16"})

	r.Use(func(c *gin.Context) {
		c.Header("X-Served-By", instanceID)
		c.Next()
	})

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":   "ok",
			"instance": instanceID,
		})
	})

	r.GET("/readyz", func(c *gin.Context) {
		checkCtx, checkCancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer checkCancel()

		dbStatus := "ok"
		redisStatus := "ok"
		isReady := true

		if err := dbPool.Ping(checkCtx); err != nil {
			dbStatus = "unreachable"
			isReady = false
		}
		if err := redisClient.Ping(checkCtx).Err(); err != nil {
			redisStatus = "unreachable"
			isReady = false
		}

		statusCode := http.StatusOK
		overall := "ready"
		if !isReady {
			statusCode = http.StatusServiceUnavailable
			overall = "not_ready"
		}

		c.JSON(statusCode, gin.H{
			"status":   overall,
			"instance": instanceID,
			"checks": gin.H{
				"postgres": dbStatus,
				"redis":    redisStatus,
			},
		})
	})

	webhookSecret := getEnv("PAYMENT_WEBHOOK_SECRET", "REDACTED")

	productHandler := handler.NewProductHandler(dbPool)
	orderHandler := handler.NewOrderHandler(dbPool)
	webhookHandler := handler.NewWebhookHandler(dbPool, redisClient, webhookSecret)
	authHandler := handler.NewAuthHandler(dbPool, redisClient, jwtSecret, smtpHost, smtpPort)
	adminHandler := handler.NewAdminHandler(dbPool, redisClient)

	v1 := r.Group("/api/v1")
	{
		v1.GET("/products", productHandler.ListProducts)
		v1.GET("/products/:slug", productHandler.GetProductBySlug)

		v1.POST("/orders", orderHandler.CreateOrder)
		v1.GET("/orders/:id", orderHandler.GetOrder)
		v1.POST("/orders/track", orderHandler.TrackOrder)
		v1.GET("/orders/:id/events", orderHandler.StreamOrderEvents)
		v1.POST("/webhooks/payment", webhookHandler.HandlePaymentWebhook)

		auth := v1.Group("/auth/otp")
		{
			auth.POST("/request", authHandler.RequestOTP)
			auth.POST("/verify", authHandler.VerifyOTP)
		}

		admin := v1.Group("/admin")
		{
			admin.GET("/orders",
				handler.RequireRoles("viewer", "support", "finance", "superadmin"),
				adminHandler.ListOrders,
			)
			admin.POST("/orders/:id/refund",
				handler.RequireRoles("finance", "superadmin"),
				adminHandler.RefundOrder,
			)
			admin.POST("/orders/:id/retry-fulfillment",
				handler.RequireRoles("support", "superadmin"),
				adminHandler.RetryFulfillment,
			)
			admin.GET("/audit-logs",
				handler.RequireRoles("viewer", "support", "finance", "superadmin"),
				adminHandler.ListAuditLogs,
			)
			admin.POST("/reconciliation",
				handler.RequireRoles("finance", "superadmin"),
				adminHandler.ReconcilePayments,
			)
		}

		if appEnv == "test" {
			v1.POST("/_test/reset", adminHandler.TestReset)
		} else {
			v1.POST("/_test/reset", func(c *gin.Context) {
				c.Status(http.StatusNotFound)
			})
		}
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	go func() {
		log.Printf("[%s] Gin API server starting on port %s (env=%s)...", instanceID, port, appEnv)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[%s] listen error: %v", instanceID, err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Printf("[%s] Shutting down server gracefully...", instanceID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("[%s] Server forced to shutdown: %v", instanceID, err)
	}
	log.Printf("[%s] Server exited cleanly", instanceID)
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func placeholderHandler(name string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusNotImplemented, gin.H{
			"message": name + " not implemented yet",
		})
	}
}
