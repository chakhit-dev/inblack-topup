package handler

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/inblack/topup-backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type AuthHandler struct {
	db        *pgxpool.Pool
	redis     *redis.Client
	jwtSecret []byte
	smtpHost  string
	smtpPort  string
}

func NewAuthHandler(db *pgxpool.Pool, rdb *redis.Client, jwtSecret, smtpHost, smtpPort string) *AuthHandler {
	return &AuthHandler{
		db:        db,
		redis:     rdb,
		jwtSecret: []byte(jwtSecret),
		smtpHost:  smtpHost,
		smtpPort:  smtpPort,
	}
}

type OTPRequestReq struct {
	Email string `json:"email" binding:"required,email"`
}

type OTPVerifyReq struct {
	Email string `json:"email" binding:"required,email"`
	Code  string `json:"code" binding:"required,len=6"`
}

type AuthClaims struct {
	Email string `json:"email"`
	Role  string `json:"role"`
	jwt.RegisteredClaims
}

// RequestOTP POST /api/v1/auth/otp/request
func (h *AuthHandler) RequestOTP(c *gin.Context) {
	var req OTPRequestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, domain.NewProblem(
			http.StatusBadRequest, domain.CodeValidationFailed, "Validation Failed", err.Error(),
		))
		return
	}

	ctx := c.Request.Context()
	email := strings.ToLower(strings.TrimSpace(req.Email))

	// Rate limit email ขอ OTP ได้ไม่เกิน 1 ครั้งต่อ 30 วินาที
	cooldownKey := fmt.Sprintf("otp:cooldown:%s", email)
	ok, err := h.redis.SetNX(ctx, cooldownKey, "1", 30*time.Second).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Redis Error", "Failed to check OTP rate limit",
		))
		return
	}
	if !ok {
		c.JSON(http.StatusTooManyRequests, domain.NewProblem(
			http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", "Too Many Requests", "Please wait 30 seconds before requesting a new OTP",
		))
		return
	}

	code, err := generate6DigitOTP()
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "OTP Generation Error", "Failed to generate OTP",
		))
		return
	}

	otpKey := fmt.Sprintf("otp:code:%s", email)
	attemptsKey := fmt.Sprintf("otp:attempts:%s", email)

	pipe := h.redis.TxPipeline()
	pipe.Set(ctx, otpKey, code, 5*time.Minute)
	pipe.Del(ctx, attemptsKey)
	if _, err := pipe.Exec(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Redis Error", "Failed to store OTP",
		))
		return
	}

	// ส่งรหัส OTP ทางอีเมล
	if err := h.sendOTPEmail(email, code); err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Email Delivery Failed", "Failed to send OTP email",
		))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":            "OTP sent to email",
		"expires_in_seconds": 300,
	})
}

// VerifyOTP POST /api/v1/auth/otp/verify
func (h *AuthHandler) VerifyOTP(c *gin.Context) {
	var req OTPVerifyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, domain.NewProblem(
			http.StatusBadRequest, domain.CodeValidationFailed, "Validation Failed", err.Error(),
		))
		return
	}

	ctx := c.Request.Context()
	email := strings.ToLower(strings.TrimSpace(req.Email))
	code := strings.TrimSpace(req.Code)

	otpKey := fmt.Sprintf("otp:code:%s", email)
	attemptsKey := fmt.Sprintf("otp:attempts:%s", email)

	storedCode, err := h.redis.Get(ctx, otpKey).Result()
	if errors.Is(err, redis.Nil) {
		c.JSON(http.StatusUnauthorized, domain.NewProblem(
			http.StatusUnauthorized, "OTP_EXPIRED_OR_INVALID", "Invalid OTP", "OTP has expired or was not requested",
		))
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Redis Error", "Failed to verify OTP",
		))
		return
	}

	// ตรวจจำนวนครั้งที่กรอกผิด (สูงสุด 5 ครั้ง)
	attempts, _ := h.redis.Incr(ctx, attemptsKey).Result()
	_ = h.redis.Expire(ctx, attemptsKey, 5*time.Minute).Err()
	if attempts > 5 {
		_ = h.redis.Del(ctx, otpKey, attemptsKey).Err()
		c.JSON(http.StatusUnauthorized, domain.NewProblem(
			http.StatusUnauthorized, "OTP_MAX_ATTEMPTS", "OTP Locked", "Too many failed OTP attempts. Please request a new OTP",
		))
		return
	}

	if storedCode != code {
		c.JSON(http.StatusUnauthorized, domain.NewProblem(
			http.StatusUnauthorized, "OTP_INVALID", "Invalid OTP", "The verification code is incorrect",
		))
		return
	}

	// ลบ OTP ทิ้งทันทีหลังใช้สำเร็จ (Single-use)
	_ = h.redis.Del(ctx, otpKey, attemptsKey).Err()

	// ตรวจสอบ Role จากตาราง users (ถ้าไม่มีในตาราง admin จะได้ role = "customer")
	role := "customer"
	var dbRole string
	err = h.db.QueryRow(ctx, `SELECT role::text FROM users WHERE LOWER(email) = $1`, email).Scan(&dbRole)
	if err == nil && dbRole != "" {
		role = dbRole
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to lookup user role",
		))
		return
	}

	now := time.Now().UTC()
	expiresAt := now.Add(24 * time.Hour)
	claims := AuthClaims{
		Email: email,
		Role:  role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   email,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString(h.jwtSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Token Error", "Failed to sign access token",
		))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token": signedToken,
		"token_type":   "Bearer",
		"expires_at":   expiresAt,
		"user": gin.H{
			"email": email,
			"role":  role,
		},
	})
}

// RequireAuth Middleware ตรวจสอบ JWT Token
func (h *AuthHandler) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, domain.NewProblem(
				http.StatusUnauthorized, "UNAUTHORIZED", "Missing Token", "Authorization Bearer token is required",
			))
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		claims := &AuthClaims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method")
			}
			return h.jwtSecret, nil
		})

		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, domain.NewProblem(
				http.StatusUnauthorized, "UNAUTHORIZED", "Invalid Token", "Token is invalid or expired",
			))
			return
		}

		c.Set("user_email", claims.Email)
		c.Set("user_role", claims.Role)
		c.Next()
	}
}

// RequireRoles Middleware ตรวจสอบสิทธิ์ตาม Role
func RequireRoles(allowedRoles ...string) gin.HandlerFunc {
	allowed := make(map[string]bool, len(allowedRoles))
	for _, r := range allowedRoles {
		allowed[r] = true
	}

	return func(c *gin.Context) {
		roleVal, exists := c.Get("user_role")
		role, _ := roleVal.(string)
		if !exists || !allowed[role] {
			c.AbortWithStatusJSON(http.StatusForbidden, domain.NewProblem(
				http.StatusForbidden, "FORBIDDEN", "Insufficient Permissions", "Your role is not authorized to access this resource",
			))
			return
		}
		c.Next()
	}
}

func generate6DigitOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func (h *AuthHandler) sendOTPEmail(toEmail, code string) error {
	addr := fmt.Sprintf("%s:%s", h.smtpHost, h.smtpPort)
	subject := "Subject: [InBlack Top-Up] Your Verification Code\r\n"
	headers := "From: no-reply@inblack.gg\r\nTo: " + toEmail + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=\"utf-8\"\r\n\r\n"
	body := fmt.Sprintf("Your OTP verification code is: %s\n\nThis code will expire in 5 minutes.", code)
	msg := []byte(subject + headers + body)
	return smtp.SendMail(addr, nil, "no-reply@inblack.gg", []string{toEmail}, msg)
}

var _ = context.Background
