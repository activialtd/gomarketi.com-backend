-- 'at_hub' completes the hub fulfilment lifecycle the consumer app models:
--
--   pending → confirmed → at_hub → shipped → delivered
--
-- The vendor hands the parcel to GoMarketi (at_hub), GoMarketi consolidates
-- every vendor's parcel for that buyer and dispatches one delivery (shipped),
-- and the buyer confirms receipt (delivered), which releases escrow.
--
-- at_hub is checked in by the platform, not the vendor, and deliberately does
-- not release escrow: the goods have reached us, not the buyer.

ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;

ALTER TABLE orders ADD CONSTRAINT orders_status_check
    CHECK (status IN ('pending', 'confirmed', 'at_hub', 'shipped', 'delivered', 'cancelled'));

ALTER TABLE orders ADD COLUMN IF NOT EXISTS hub_received_at TIMESTAMPTZ;
