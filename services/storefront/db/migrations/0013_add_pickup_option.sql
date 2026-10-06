-- Let a vendor offer collection instead of delivery.
--
-- Pickup rides on the delivery options a vendor already configures rather
-- than becoming a parallel concept: the buyer picks it the same way, it is
-- priced the same way (at zero), and it travels through checkout on the same
-- delivery_option_id. The alternative — a separate flag on the store plus its
-- own branch at every step — would have meant two things to keep in step for
-- one question, "how is this order getting to the buyer".
ALTER TABLE store_delivery_options
    ADD COLUMN IF NOT EXISTS is_pickup BOOLEAN NOT NULL DEFAULT FALSE;

-- Collection is free by definition: nobody is being paid to carry it.
ALTER TABLE store_delivery_options
    ADD CONSTRAINT store_delivery_options_pickup_is_free
    CHECK (NOT is_pickup OR price_kobo = 0);

-- A store collects at one place, so one pickup option. Partial, so it does
-- not constrain ordinary delivery rows at all.
CREATE UNIQUE INDEX IF NOT EXISTS idx_delivery_options_one_pickup
    ON store_delivery_options (store_id)
    WHERE is_pickup;
