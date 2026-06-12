CREATE TABLE IF NOT EXISTS stores (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vendor_id     UUID        NOT NULL UNIQUE,
    name          TEXT        NOT NULL,
    slug          TEXT        NOT NULL UNIQUE,
    category      TEXT        NOT NULL,
    currency      TEXT        NOT NULL DEFAULT 'NGN',
    team_size     TEXT,
    staff_range   TEXT,
    tagline       TEXT,
    logo_url      TEXT,
    support_phone TEXT,
    address       TEXT,
    city          TEXT,
    state         TEXT,
    is_active     BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_stores_vendor_id ON stores (vendor_id);
CREATE INDEX IF NOT EXISTS idx_stores_slug      ON stores (slug);
