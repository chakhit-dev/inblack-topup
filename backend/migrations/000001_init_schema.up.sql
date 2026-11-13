-- 1. Enums
DO $$ BEGIN
    CREATE TYPE role AS ENUM ('viewer', 'support', 'finance', 'superadmin');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$ BEGIN
    CREATE TYPE discount_type AS ENUM ('PERCENTAGE', 'FIXED');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$ BEGIN
    CREATE TYPE order_status AS ENUM (
        'PENDING_PAYMENT',
        'PAID',
        'IN_QUEUE',
        'IN_PROGRESS',
        'COMPLETED',
        'EXPIRED',
        'PAYMENT_FAILED',
        'FAILED',
        'REFUNDED',
        'NEEDS_REVIEW'
    );
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

-- 2. Products (แยก name_th / name_en ตามที่คุณออกแบบ)
CREATE TABLE IF NOT EXISTS products (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sku VARCHAR(50) UNIQUE NOT NULL,
    slug VARCHAR(100) UNIQUE NOT NULL,
    name_th VARCHAR(255) NOT NULL,
    name_en VARCHAR(255) NOT NULL,
    price BIGINT NOT NULL CHECK (price >= 0),
    stock INTEGER NULL CHECK (stock >= 0),
    limit_per_order INTEGER NULL,
    limit_per_email INTEGER NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 3. Admin Users
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role role NOT NULL DEFAULT 'viewer',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 4. Vouchers
CREATE TABLE IF NOT EXISTS vouchers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(64) NOT NULL UNIQUE,
    type discount_type NOT NULL,
    discount_value BIGINT NOT NULL CHECK (discount_value > 0),
    max_discount BIGINT CHECK (max_discount IS NULL OR max_discount > 0),
    min_subtotal BIGINT NOT NULL DEFAULT 0 CHECK (min_subtotal >= 0),
    usage_limit INT CHECK (usage_limit IS NULL OR usage_limit > 0),
    used_count INT NOT NULL DEFAULT 0 CHECK (
        used_count >= 0 AND (usage_limit IS NULL OR used_count <= usage_limit)
    ),
    limit_per_email INT CHECK (limit_per_email IS NULL OR limit_per_email > 0),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 5. Orders
CREATE TABLE IF NOT EXISTS orders (
    id VARCHAR(21) PRIMARY KEY,
    seq_id BIGSERIAL UNIQUE NOT NULL,
    customer_email VARCHAR(255) NOT NULL,
    game_uid VARCHAR(100) NOT NULL,
    payment_method VARCHAR(30) NOT NULL,
    voucher_code VARCHAR(64) NULL REFERENCES vouchers(code),

    subtotal BIGINT NOT NULL CHECK (subtotal >= 0),
    discount BIGINT NOT NULL DEFAULT 0 CHECK (discount >= 0),
    net BIGINT NOT NULL CHECK (net >= 0),
    fee BIGINT NOT NULL DEFAULT 0 CHECK (fee >= 0),
    total BIGINT NOT NULL CHECK (total >= 0),
    vat BIGINT NOT NULL DEFAULT 0 CHECK (vat >= 0),
    refunded_amount BIGINT NOT NULL DEFAULT 0 CHECK (refunded_amount >= 0 AND refunded_amount <= total),

    status order_status NOT NULL DEFAULT 'PENDING_PAYMENT',
    expires_at TIMESTAMPTZ NOT NULL,
    processing_started_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_orders_status_expires_at ON orders (status, expires_at) WHERE status = 'PENDING_PAYMENT';
CREATE INDEX IF NOT EXISTS idx_orders_status_processing ON orders (status, processing_started_at) WHERE status = 'IN_PROGRESS';
CREATE INDEX IF NOT EXISTS idx_orders_cursor_created ON orders (created_at DESC, seq_id DESC);
CREATE INDEX IF NOT EXISTS idx_orders_status_cursor ON orders (status, created_at DESC, seq_id DESC);
CREATE INDEX IF NOT EXISTS idx_orders_customer_email ON orders (customer_email, created_at DESC);

-- 6. Order Items
CREATE TABLE IF NOT EXISTS order_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id VARCHAR(21) NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id),
    sku VARCHAR(50) NOT NULL,
    product_name_th VARCHAR(255) NOT NULL,
    product_name_en VARCHAR(255) NOT NULL,
    unit_price BIGINT NOT NULL CHECK (unit_price >= 0),
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    total_price BIGINT NOT NULL CHECK (total_price >= 0)
);

CREATE INDEX IF NOT EXISTS idx_order_items_order_id ON order_items (order_id);
CREATE INDEX IF NOT EXISTS idx_order_items_product_id ON order_items (product_id);

-- 7. Voucher Usages
CREATE TABLE IF NOT EXISTS voucher_usages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    voucher_id UUID NOT NULL REFERENCES vouchers(id),
    order_id VARCHAR(21) NOT NULL UNIQUE REFERENCES orders(id) ON DELETE CASCADE,
    customer_email VARCHAR(255) NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_voucher_usages_active_per_email 
ON voucher_usages (voucher_id, customer_email) 
WHERE is_active = TRUE;

-- 8. Order Events
CREATE TABLE IF NOT EXISTS order_events (
    id BIGSERIAL PRIMARY KEY,
    order_id VARCHAR(21) NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    from_status order_status NULL,
    to_status order_status NOT NULL,
    actor VARCHAR(100) NOT NULL,
    reason TEXT NOT NULL,
    metadata JSONB NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_order_events_order_id_id ON order_events (order_id, id ASC);

-- 9. Webhook Events
CREATE TABLE IF NOT EXISTS webhook_events (
    event_id VARCHAR(100) PRIMARY KEY,
    order_id VARCHAR(21) NOT NULL,
    event_type VARCHAR(50) NOT NULL,
    amount BIGINT NOT NULL,
    raw_payload JSONB NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'PROCESSED',
    occurred_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhook_events_order_id ON webhook_events (order_id, received_at ASC);

-- 10. Fulfillment Attempts
CREATE TABLE IF NOT EXISTS fulfillment_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id VARCHAR(21) NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    reference VARCHAR(64) NOT NULL UNIQUE,
    attempt_number INT NOT NULL DEFAULT 1,
    status VARCHAR(30) NOT NULL DEFAULT 'PENDING',
    provider_response JSONB NULL,
    error_message TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_fulfillment_one_per_order ON fulfillment_attempts (order_id);

-- 11. Refunds
CREATE TABLE IF NOT EXISTS refunds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id VARCHAR(21) NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    idempotency_key VARCHAR(100) NOT NULL UNIQUE,
    amount BIGINT NOT NULL CHECK (amount > 0),
    reason TEXT NOT NULL,
    refunded_by VARCHAR(255) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'SUCCEEDED',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_refunds_order_id ON refunds (order_id);

-- 12. State Machine Trigger
CREATE OR REPLACE FUNCTION enforce_order_status_transition()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.status = NEW.status THEN
        RETURN NEW;
    END IF;

    IF NEW.status = 'NEEDS_REVIEW' THEN
        RETURN NEW;
    END IF;

    IF (OLD.status = 'PENDING_PAYMENT' AND NEW.status IN ('PAID', 'EXPIRED', 'PAYMENT_FAILED')) OR
       (OLD.status = 'EXPIRED'         AND NEW.status = 'PAID') OR
       (OLD.status = 'PAID'            AND NEW.status = 'IN_QUEUE') OR
       (OLD.status = 'IN_QUEUE'        AND NEW.status = 'IN_PROGRESS') OR
       (OLD.status = 'IN_PROGRESS'     AND NEW.status IN ('COMPLETED', 'FAILED', 'IN_QUEUE')) OR
       (OLD.status = 'FAILED'          AND NEW.status IN ('REFUNDED', 'IN_QUEUE')) OR
       (OLD.status = 'COMPLETED'       AND NEW.status = 'REFUNDED') THEN
        RETURN NEW;
    END IF;

    RAISE EXCEPTION 'INVALID_TRANSITION: cannot transition order % from % to %', OLD.id, OLD.status, NEW.status;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_enforce_order_status ON orders;
CREATE TRIGGER trg_enforce_order_status
BEFORE UPDATE OF status ON orders
FOR EACH ROW
EXECUTE FUNCTION enforce_order_status_transition();