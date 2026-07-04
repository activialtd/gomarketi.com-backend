-- Track storefront page views per store (used in analytics overview)
CREATE TABLE IF NOT EXISTS storefront_visits (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id   UUID        NOT NULL,
    session_id TEXT        NOT NULL,
    page       TEXT        NOT NULL DEFAULT '/',
    visited_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_storefront_visits_store ON storefront_visits (store_id, visited_at DESC);

-- Discount amount applied at checkout (0 when no discount used)
ALTER TABLE orders ADD COLUMN IF NOT EXISTS discount_kobo BIGINT NOT NULL DEFAULT 0;

-- How the order was paid and which gateway processed it
ALTER TABLE orders ADD COLUMN IF NOT EXISTS payment_method  TEXT NOT NULL DEFAULT 'online';
ALTER TABLE orders ADD COLUMN IF NOT EXISTS payment_gateway TEXT;
