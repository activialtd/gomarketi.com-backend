-- Create the order before taking the money, not after.
--
-- Checkout used to charge Paystack first and save the order second, so any
-- interruption in that gap took money and left nothing behind. The previous
-- migration's checkout_intents softened that by storing the intended order up
-- front, but recovery still had to *create* the order afterwards — and
-- creation can legitimately refuse (a product deleted since, a plan limit, a
-- deactivated store), which put us back to money with no order.
--
-- Order-first removes that class of failure rather than making it rarer. The
-- row exists before any charge, and payment confirmation is a status change,
-- which has no business rules left to fail.
--
--   awaiting_payment  created, payment not yet confirmed
--   pending           paid; the vendor has yet to confirm it
--   abandoned         created but never paid for
--
-- Unpaid orders are marked abandoned rather than cancelled: the customer got
-- as far as entering their details and picking delivery, which is exactly
-- what the vendor's abandoned page is for. Cancelling would throw that away.
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check
    CHECK (status IN ('awaiting_payment','pending','confirmed','at_hub','shipped','delivered','cancelled','abandoned'));

-- When the payment was confirmed. NULL on an order that was never paid for,
-- which is what separates abandoned from merely new.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS paid_at TIMESTAMPTZ;

-- Existing rows all predate this: every one of them was created only after a
-- verified charge, so they are paid by definition.
UPDATE orders SET paid_at = created_at WHERE paid_at IS NULL AND status <> 'cancelled';

-- The reaper's query: unpaid orders, oldest first.
CREATE INDEX IF NOT EXISTS idx_orders_awaiting_payment
    ON orders (created_at)
    WHERE status = 'awaiting_payment';

-- The vendor's abandoned list, newest first per store.
CREATE INDEX IF NOT EXISTS idx_orders_abandoned
    ON orders (store_id, created_at DESC)
    WHERE status = 'abandoned';

-- checkout_intents is superseded: the order row is now the record of intent.
-- Dropped rather than left behind so there is one place a pending checkout
-- lives, not two that can disagree.
DROP TABLE IF EXISTS checkout_intents;
