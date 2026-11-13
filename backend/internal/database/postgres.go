package database

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func NewPostgresPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	cfg.MaxConns = 25
	cfg.MinConns = 5
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}

func RunMigrationAndSeed(ctx context.Context, pool *pgxpool.Pool, migrationPath string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	const lockID = 997001
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockID); err != nil {
		return fmt.Errorf("acquire advisory lock: %w", err)
	}
	defer conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", lockID)

	sqlBytes, err := os.ReadFile(migrationPath)
	if err != nil {
		return fmt.Errorf("read migration file %s: %w", migrationPath, err)
	}

	if _, err := conn.Exec(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("execute migration: %w", err)
	}

	if err := seedInitialData(ctx, pool); err != nil {
		return fmt.Errorf("seed initial data: %w", err)
	}

	log.Println("Database migration and seeding completed successfully")
	return nil
}

func seedInitialData(ctx context.Context, pool *pgxpool.Pool) error {
	productsSQL := `
		INSERT INTO products (
			sku, slug, name_th, name_en, price, stock, limit_per_order, limit_per_email
		) VALUES
			('DIAMOND-1000', '1000-diamonds', '1,000 เพชร', '1,000 Diamonds', 34900, NULL, NULL, NULL),
			('DIAMOND-5000', '5000-diamonds', '5,000 เพชร', '5,000 Diamonds', 159000, NULL, NULL, NULL),
			('MONTHLY-PASS', 'monthly-pass', 'บัตรรายเดือน', 'Monthly Pass', 17900, NULL, 1, NULL),
			('FLASH-SKIN-01', 'limited-skin-flash-sale', 'สกินลิมิเต็ด (Flash Sale)', 'Limited Skin (Flash Sale)', 9900, 100, NULL, 2)
		ON CONFLICT (sku) DO NOTHING;
	`
	if _, err := pool.Exec(ctx, productsSQL); err != nil {
		return err
	}

	vouchersSQL := `
		INSERT INTO vouchers (code, type, discount_value, max_discount, min_subtotal, usage_limit, limit_per_email)
		VALUES
			('SAVE10', 'PERCENTAGE', 10, 5000, 20000, 10, 1),
			('FLAT20', 'FIXED', 2000, NULL, 10000, NULL, NULL)
		ON CONFLICT (code) DO NOTHING;
	`
	if _, err := pool.Exec(ctx, vouchersSQL); err != nil {
		return err
	}

	defaultPass, err := bcrypt.GenerateFromPassword([]byte("Admin1234!"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	usersSQL := `
		INSERT INTO users (email, password_hash, role)
		VALUES
			('viewer@inblackm.com', $1, 'viewer'),
			('support@inblackm.com', $1, 'support'),
			('finance@inblackm.com', $1, 'finance'),
			('admin@inblackm.com', $1, 'superadmin')
		ON CONFLICT (email) DO NOTHING;
	`
	_, err = pool.Exec(ctx, usersSQL, string(defaultPass))
	return err
}
