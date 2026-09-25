-- A dispute: the buyer says a dispatched order never arrived.
--
-- Deliberately orthogonal to status — the order stays 'shipped', because
-- fulfilment did happen from the vendor's side. What changes is that the
-- money stops moving: a reported order is skipped by the escrow auto-release
-- sweep, so the vendor is not paid while the claim is open.
--
-- Resolution is manual (admin decides refunded or dismissed), and the refund
-- itself is a Paystack operation done by hand.

ALTER TABLE orders ADD COLUMN IF NOT EXISTS dispute_status TEXT
    CHECK (dispute_status IS NULL OR dispute_status IN ('reported', 'refunded', 'dismissed'));
ALTER TABLE orders ADD COLUMN IF NOT EXISTS dispute_reason TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS disputed_at    TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_orders_disputes ON orders (dispute_status)
    WHERE dispute_status IS NOT NULL;
