-- Canonical products give "the same product sold by different vendors" a
-- real identity, instead of a coincidence of shared words in free-text
-- names — needed so cross-vendor search can group listings by product,
-- not just by ILIKE-matching whatever string each vendor happened to type.
-- Linking is human-in-the-loop at product-create/edit time (a vendor picks
-- a suggested canonical product or the system mints a new one on submit) —
-- never an automatic background guess, which would risk silently merging
-- distinct products (e.g. "iPhone 14" and "iPhone 14 Pro").
--
-- First use of pg_trgm in this codebase (PostGIS is the only prior
-- extension precedent, in the storefront service).
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS canonical_products (
    id                    UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name                  TEXT        NOT NULL,
    normalized_name       TEXT        NOT NULL, -- lower(trim(name)), a real column so the
                                                  -- trigram index doesn't recompute lower() per query
    representative_image  TEXT,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_canonical_products_name_trgm
    ON canonical_products USING GIN (normalized_name gin_trgm_ops);

ALTER TABLE products ADD COLUMN IF NOT EXISTS canonical_product_id UUID
    REFERENCES canonical_products(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_products_canonical_product_id ON products (canonical_product_id);
