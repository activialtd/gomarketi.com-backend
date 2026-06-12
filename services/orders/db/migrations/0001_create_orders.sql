CREATE TABLE IF NOT EXISTS orders (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id         UUID        NOT NULL,
    customer_id      UUID        NOT NULL,
    customer_name    TEXT        NOT NULL DEFAULT '',
    customer_email   TEXT        NOT NULL DEFAULT '',
    status           TEXT        NOT NULL DEFAULT 'pending'
                                 CHECK (status IN ('pending','confirmed','shipped','delivered','cancelled')),
    total_kobo       BIGINT      NOT NULL DEFAULT 0,
    delivery_address TEXT        NOT NULL DEFAULT '',
    note             TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_orders_store_id   ON orders (store_id);
CREATE INDEX IF NOT EXISTS idx_orders_customer   ON orders (store_id, customer_id);
CREATE INDEX IF NOT EXISTS idx_orders_status     ON orders (store_id, status);
CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders (store_id, created_at DESC);

CREATE TABLE IF NOT EXISTS order_items (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id    UUID    NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    product_id  UUID    NOT NULL,
    name        TEXT    NOT NULL,
    image_url   TEXT    NOT NULL DEFAULT '',
    quantity    INTEGER NOT NULL CHECK (quantity > 0),
    price_kobo  BIGINT  NOT NULL CHECK (price_kobo >= 0)
);

CREATE INDEX IF NOT EXISTS idx_order_items_order_id ON order_items (order_id);

CREATE TABLE IF NOT EXISTS abandoned_carts (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id       UUID        NOT NULL,
    customer_id    UUID,
    customer_email TEXT,
    items          JSONB       NOT NULL DEFAULT '[]',
    total_kobo     BIGINT      NOT NULL DEFAULT 0,
    abandoned_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_abandoned_carts_store_id ON abandoned_carts (store_id);
