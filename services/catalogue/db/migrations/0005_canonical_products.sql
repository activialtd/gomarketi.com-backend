-- Canonical products: the shared identity behind "the same item, sold by
-- several vendors".
--
-- A vendor links their listing to a canonical entry while creating it (the
-- typeahead in the product form), which is what lets the consumer app group
-- listings across vendors — the "who else has this" carousel.
--
-- The table starts empty: entries are curated rather than created by vendors,
-- so a vendor cannot fragment the catalogue by inventing near-duplicates.
-- Until it has rows the typeahead simply returns nothing and products save
-- with a null link, which is the same behaviour as before.

CREATE TABLE IF NOT EXISTS canonical_products (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                 TEXT        NOT NULL,
    representative_image TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Typeahead search is trigram-matched, same as product search.
CREATE INDEX IF NOT EXISTS idx_canonical_products_name_trgm
    ON canonical_products USING GIN (LOWER(name) gin_trgm_ops);

ALTER TABLE products ADD COLUMN IF NOT EXISTS canonical_product_id UUID
    REFERENCES canonical_products (id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_products_canonical ON products (canonical_product_id);
