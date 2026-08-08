-- Idempotency guard for checkout: a payment_reference can only ever be
-- claimed once. Multi-store checkout deliberately creates N order rows
-- (one per vendor) sharing a single reference, so the uniqueness lives here
-- rather than on orders.payment_reference itself — inserting the claim row
-- happens inside the same transaction as the order writes it guards, so a
-- committed claim guarantees the corresponding order(s) exist.
CREATE TABLE IF NOT EXISTS checkout_payments (
    payment_reference TEXT        PRIMARY KEY,
    amount_kobo       BIGINT      NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE orders ADD COLUMN IF NOT EXISTS payment_reference TEXT;

CREATE INDEX IF NOT EXISTS idx_orders_payment_reference ON orders (payment_reference);
