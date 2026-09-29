-- 0008_add_delivery_disputes.sql
-- Closes a real gap in the hub model: the no-show/refund path only ever
-- covers a vendor who never checked their item in at the hub BEFORE
-- dispatch. There was no path at all for the opposite case — an item that
-- WAS checked in and WAS dispatched, but the buyer says it never actually
-- arrived. Previously that meant the 7-day auto-release would blindly pay
-- the vendor with zero awareness a dispute existed.
--
-- dispute_status is deliberately orthogonal to orders.status — a disputed
-- order stays 'shipped'/'delivered' for fulfillment-tracking purposes; the
-- dispute is a parallel flag admin resolves by either refunding the buyer
-- (dispute_status='refunded', same real-Paystack-refund mechanism as the
-- no-show path) or dismissing it (dispute_status='dismissed', e.g. resolved
-- directly with the customer, or found to be in error).

ALTER TABLE orders
    ADD COLUMN dispute_status      TEXT CHECK (dispute_status IN ('reported','refunded','dismissed')),
    ADD COLUMN dispute_reason      TEXT,
    ADD COLUMN disputed_at         TIMESTAMPTZ,
    ADD COLUMN dispute_resolved_at TIMESTAMPTZ,
    ADD COLUMN dispute_resolved_by UUID; -- admin_users.id — no FK, cross-service-owned table

CREATE INDEX idx_orders_disputed
    ON orders (disputed_at)
    WHERE dispute_status = 'reported';
