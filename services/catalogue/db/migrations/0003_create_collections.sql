CREATE TABLE IF NOT EXISTS collections (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id     UUID        NOT NULL,
    name         TEXT        NOT NULL,
    description  TEXT,
    image_url    TEXT,
    is_published BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (store_id, name)
);

CREATE TABLE IF NOT EXISTS collection_products (
    collection_id UUID NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    product_id    UUID NOT NULL REFERENCES products(id)    ON DELETE CASCADE,
    position      INTEGER NOT NULL DEFAULT 0,
    added_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (collection_id, product_id)
);

CREATE INDEX IF NOT EXISTS idx_collections_store_id ON collections (store_id);
CREATE INDEX IF NOT EXISTS idx_collection_products_product ON collection_products (product_id);
