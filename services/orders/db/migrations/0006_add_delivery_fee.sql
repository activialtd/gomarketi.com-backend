-- Delivery fee charged on an order.
--
-- The fee comes from the vendor's own store_delivery_options row (storefront
-- service, same database). Both the amount and the option title are copied
-- onto the order so that editing or deleting an option later never rewrites
-- what a past order charged.
--
-- total_kobo stays the full amount charged: line items + delivery_fee_kobo.

ALTER TABLE orders ADD COLUMN IF NOT EXISTS delivery_fee_kobo BIGINT NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS delivery_option_title TEXT NOT NULL DEFAULT '';
