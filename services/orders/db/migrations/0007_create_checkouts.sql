-- A checkout is one payment that produced one or more orders.
--
-- The consumer app's cart can span several vendors: each vendor gets their
-- own order (they fulfil separately, and escrow/payout is per vendor), but
-- the buyer paid once and — under the hub model — receives one consolidated
-- delivery. So the delivery fee belongs to the checkout, not to any single
-- vendor's order, and is charged exactly once no matter how many vendors are
-- in the basket.
--
-- total_kobo here is the full amount charged: every order's items plus
-- delivery_fee_kobo. It is what Paystack is verified against.

CREATE TABLE IF NOT EXISTS checkouts (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_name         TEXT        NOT NULL,
    customer_email        TEXT        NOT NULL,
    customer_phone        TEXT        NOT NULL DEFAULT '',
    delivery_address      TEXT        NOT NULL DEFAULT '',
    -- Unique so a retried checkout (same Paystack reference) returns the
    -- orders already created instead of charging the buyer's cart twice.
    payment_reference     TEXT        NOT NULL UNIQUE,
    delivery_option_id    UUID,
    delivery_option_title TEXT        NOT NULL DEFAULT '',
    delivery_fee_kobo     BIGINT      NOT NULL DEFAULT 0 CHECK (delivery_fee_kobo >= 0),
    items_kobo            BIGINT      NOT NULL DEFAULT 0 CHECK (items_kobo >= 0),
    total_kobo            BIGINT      NOT NULL DEFAULT 0 CHECK (total_kobo >= 0),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Nullable: single-store orders from the storefront checkout have no parent.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS checkout_id UUID REFERENCES checkouts (id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_orders_checkout_id ON orders (checkout_id);
CREATE INDEX IF NOT EXISTS idx_checkouts_email    ON checkouts (LOWER(customer_email));
