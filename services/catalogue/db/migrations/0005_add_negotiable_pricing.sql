-- Negotiable pricing: a vendor can mark a product open to offers and set the
-- lowest price they will take for it.
--
-- price_kobo stays the list price buyers see. min_price_kobo is the vendor's
-- private floor — the bottom of their range — and is never returned on a
-- public endpoint: an offer at or above it is accepted instantly, anything
-- lower goes to the vendor (see the orders service's price_offers).
ALTER TABLE products
    ADD COLUMN IF NOT EXISTS is_negotiable  BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS min_price_kobo BIGINT;

-- A floor only means something on a negotiable product, and has to sit below
-- the list price, or every offer would clear it.
DO $$
BEGIN
    ALTER TABLE products ADD CONSTRAINT products_negotiable_floor CHECK (
        (is_negotiable = FALSE AND min_price_kobo IS NULL)
        OR (is_negotiable = TRUE AND min_price_kobo > 0 AND min_price_kobo < price_kobo)
    );
EXCEPTION WHEN duplicate_object THEN NULL;
END
$$;
