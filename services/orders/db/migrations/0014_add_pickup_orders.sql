-- Record that an order is being collected rather than delivered.
--
-- Derived from the delivery option the buyer chose (store_delivery_options
-- .is_pickup) and frozen onto the order, the same way the fee is: the option
-- can be renamed or removed later, and an order has to keep saying how it was
-- meant to reach the buyer.
--
-- It matters to the vendor most of all. A pickup order they treat as a
-- delivery is a parcel sent to someone who is on their way to the shop.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS is_pickup BOOLEAN NOT NULL DEFAULT FALSE;

-- Collection is free, so an order can never be both.
ALTER TABLE orders ADD CONSTRAINT orders_pickup_has_no_fee
    CHECK (NOT is_pickup OR delivery_fee_kobo = 0);
