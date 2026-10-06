-- A status for an order waiting on the counter.
--
-- Pickup orders had to borrow 'shipped', which told the buyer their order was
-- "on its way" when it was sitting in the shop waiting for them, and asked the
-- vendor to claim a dispatch that never happened.
--
-- It sits where shipped sits in the lifecycle — the vendor has done their part
-- and the order is waiting on the buyer — so it starts the same escrow clock.
-- A buyer who never comes to collect must not leave the vendor unpaid forever.
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check
    CHECK (status IN ('awaiting_payment','pending','confirmed','at_hub','shipped','ready_for_collection','delivered','cancelled','abandoned'));
