-- 0007_add_hub_escrow.sql
-- Hub-and-spoke fulfillment: vendors deliver their portion of an order to a
-- central GoMarketi office; admin checks each vendor's delivery in, then
-- dispatches the consolidated batch (all orders sharing one
-- payment_reference) to the customer. A vendor who doesn't deliver in time
-- gets their order cancelled and refunded instead of dispatched. Escrow
-- (wallet_transactions.status) only releases once the buyer confirms
-- receipt, or a 7-day auto-release fallback fires — see
-- services/orders/internal/service/orders.go.

ALTER TABLE orders DROP CONSTRAINT orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check
    CHECK (status IN ('pending','confirmed','at_hub','shipped','delivered','cancelled'));

ALTER TABLE orders
    ADD COLUMN hub_received_at      TIMESTAMPTZ,
    ADD COLUMN hub_received_by      UUID,   -- admin_users.id — no FK, cross-service-owned table
    ADD COLUMN dispatched_at        TIMESTAMPTZ,
    ADD COLUMN delivered_at         TIMESTAMPTZ,   -- customer received (buyer-confirmed or auto-released)
    ADD COLUMN delivery_confirmed_at TIMESTAMPTZ,  -- the actual escrow-release trigger
    ADD COLUMN cancelled_reason     TEXT,
    ADD COLUMN refund_reference     TEXT,
    ADD COLUMN refunded_at          TIMESTAMPTZ;

ALTER TABLE wallet_transactions
    ADD COLUMN released_at TIMESTAMPTZ;

CREATE INDEX idx_orders_dispatched_pending_confirm
    ON orders (dispatched_at)
    WHERE status = 'shipped' AND delivery_confirmed_at IS NULL;
