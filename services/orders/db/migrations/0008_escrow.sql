-- Escrow: the vendor's sale credit is held until the buyer has the goods.
--
-- The platform takes the buyer's payment up front and delivers through the
-- hub, so the money sits with the platform while the parcel is in transit.
-- Until this migration the sale credit was written 'completed' immediately,
-- which meant a vendor could withdraw before shipping anything — leaving the
-- platform to fund any refund out of its own pocket.
--
-- No new money store is needed: wallet_transactions.status already has a
-- 'pending' state and GetWallet already sums only 'completed' rows. Held
-- escrow is a pending credit; releasing it flips that row to completed.
--
--   held     → credit is pending, not withdrawable
--   released → buyer confirmed (or auto-release elapsed); credit completed
--   reversed → order cancelled or never arrived; credit failed, buyer refunded

ALTER TABLE orders ADD COLUMN IF NOT EXISTS escrow_status TEXT NOT NULL DEFAULT 'held'
    CHECK (escrow_status IN ('held', 'released', 'reversed'));

-- Fulfilment timestamps the consumer app already expects on an order.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS dispatched_at         TIMESTAMPTZ;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS delivered_at          TIMESTAMPTZ;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS delivery_confirmed_at TIMESTAMPTZ;

-- Orders that predate escrow already have completed credits in the ledger.
-- Marking them 'held' would misreport money the vendor can already withdraw,
-- so they start as released. Only orders created from now on are held.
UPDATE orders SET escrow_status = 'released' WHERE escrow_status = 'held';

-- Drives the auto-release sweep: held orders, oldest dispatch first.
CREATE INDEX IF NOT EXISTS idx_orders_escrow_release
    ON orders (escrow_status, dispatched_at)
    WHERE escrow_status = 'held';
