CREATE TABLE IF NOT EXISTS storefront_views (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id    UUID        NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    slug        TEXT        NOT NULL,
    path        TEXT        NOT NULL DEFAULT '/',
    referrer    TEXT,
    ip_hash     TEXT,
    viewed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_storefront_views_store_id
    ON storefront_views (store_id, viewed_at DESC);

CREATE INDEX IF NOT EXISTS idx_storefront_views_viewed_at
    ON storefront_views (viewed_at DESC);
