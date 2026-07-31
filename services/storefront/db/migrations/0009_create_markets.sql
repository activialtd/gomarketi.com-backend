-- 0009_create_markets.sql
-- Major physical markets (Balogun, Alaba, Onitsha Main Market, etc.) that a
-- store can optionally belong to. Lets the consumer app answer queries like
-- "men's wear in balogun" precisely, instead of only by category+distance.
--
-- aliases holds alternate spellings/short names a buyer might type/say
-- (e.g. "computer village" market also matching "cv"), checked alongside
-- name during search-query parsing.

CREATE TABLE markets (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT        NOT NULL,
    city       TEXT        NOT NULL,
    state      TEXT        NOT NULL,
    aliases    TEXT[]      NOT NULL DEFAULT '{}',
    coordinates GEOMETRY(POINT, 4326),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (name, state)
);

CREATE INDEX idx_markets_state ON markets (state);
CREATE INDEX idx_markets_city  ON markets (city);

ALTER TABLE stores ADD COLUMN market_id UUID REFERENCES markets(id) ON DELETE SET NULL;
CREATE INDEX idx_stores_market_id ON stores (market_id);
