CREATE TABLE IF NOT EXISTS products (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id     UUID        NOT NULL,
    name         TEXT        NOT NULL,
    description  TEXT,
    category_id  UUID        REFERENCES categories(id) ON DELETE SET NULL,
    price_kobo   BIGINT      NOT NULL DEFAULT 0 CHECK (price_kobo >= 0),
    stock        INTEGER     NOT NULL DEFAULT 0 CHECK (stock >= 0),
    sku          TEXT,
    images       TEXT[]      NOT NULL DEFAULT '{}',
    tags         TEXT[]      NOT NULL DEFAULT '{}',
    is_digital   BOOLEAN     NOT NULL DEFAULT FALSE,
    is_published BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (store_id, sku)
);

CREATE INDEX IF NOT EXISTS idx_products_store_id    ON products (store_id);
CREATE INDEX IF NOT EXISTS idx_products_category_id ON products (category_id);
CREATE INDEX IF NOT EXISTS idx_products_published   ON products (store_id, is_published);
