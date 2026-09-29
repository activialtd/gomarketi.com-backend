-- 0009_create_markets.sql
--
-- Every statement here is idempotent because this migration collides with
-- 0008_add_markets_and_delivery_options.sql, which creates the same table
-- from the other side of the staging merge. Both are already recorded as
-- applied on deployed databases, but on a fresh one they run in filename
-- order and the second used to abort with "relation markets already exists",
-- taking storefront's startup down with it. 0012 reconciles the two schemas.
-- Major physical markets (Balogun, Alaba, Onitsha Main Market, etc.) that a
-- store can optionally belong to. Lets the consumer app answer queries like
-- "men's wear in balogun" precisely, instead of only by category+distance.
--
-- aliases holds alternate spellings/short names a buyer might type/say
-- (e.g. "computer village" market also matching "cv"), checked alongside
-- name during search-query parsing.

CREATE TABLE IF NOT EXISTS markets (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT        NOT NULL,
    city       TEXT        NOT NULL,
    state      TEXT        NOT NULL,
    aliases    TEXT[]      NOT NULL DEFAULT '{}',
    coordinates GEOMETRY(POINT, 4326),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (name, state)
);

CREATE INDEX IF NOT EXISTS idx_markets_state ON markets (state);
CREATE INDEX IF NOT EXISTS idx_markets_city  ON markets (city);

ALTER TABLE stores ADD COLUMN IF NOT EXISTS market_id UUID REFERENCES markets(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_stores_market_id ON stores (market_id);
