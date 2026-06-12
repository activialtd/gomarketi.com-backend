CREATE TABLE IF NOT EXISTS store_staff (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id   UUID        NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL,
    full_name  TEXT        NOT NULL DEFAULT '',
    email      TEXT        NOT NULL,
    role       TEXT        NOT NULL CHECK (role IN ('manager','staff')),
    invited_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (store_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_store_staff_store_id ON store_staff (store_id);
