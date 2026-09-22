-- Trigram indexes so vendor search tolerates misspellings the same way
-- product search does. pg_trgm is created by the catalogue service too —
-- whichever service starts first wins, and IF NOT EXISTS makes that safe.

CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS unaccent;

CREATE INDEX IF NOT EXISTS idx_stores_name_trgm    ON stores USING GIN (LOWER(name) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_stores_tagline_trgm ON stores USING GIN (LOWER(COALESCE(tagline, '')) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_markets_name_trgm   ON markets USING GIN (LOWER(name) gin_trgm_ops);
