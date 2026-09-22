-- Search support: typo-tolerant matching over products.
--
-- Two complementary mechanisms:
--   pg_trgm  — trigram similarity, which matches misspellings and partial
--              words ("jollof ric" → "Jollof Rice") and is what makes the
--              search forgiving rather than exact.
--   tsvector — word-level full-text over name + description + tags, which
--              handles multi-word queries and ranks by term frequency.
--
-- unaccent is applied at query time rather than in the index so the same
-- expression works for both mechanisms.

CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS unaccent;

-- The `%` operator is the only trigram comparison Postgres can answer from a
-- GIN index, and it uses this threshold. The default (0.3) is too strict for
-- short Nigerian product names — "rise" would not reach "rice" — so lower it
-- for the whole database. Falling back to the default is harmless if the
-- role cannot alter the database (managed Postgres), just a little stricter.
DO $$
BEGIN
    EXECUTE format('ALTER DATABASE %I SET pg_trgm.similarity_threshold = 0.2',
                   current_database());
EXCEPTION WHEN insufficient_privilege THEN
    RAISE NOTICE 'could not set pg_trgm.similarity_threshold — using the default';
END
$$;

-- search_doc is maintained by Postgres — no application code writes it.
-- 'simple' rather than 'english': product names here are largely Nigerian
-- brand and food terms that an English stemmer mangles.
--
-- Tags are deliberately not in this column: array_to_string is only STABLE,
-- not IMMUTABLE, so Postgres rejects it in a generated expression. Tag
-- matching happens separately against the tags array, which the GIN index
-- below covers.
ALTER TABLE products ADD COLUMN IF NOT EXISTS search_doc tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', COALESCE(name, '')), 'A') ||
        setweight(to_tsvector('simple', COALESCE(description, '')), 'C')
    ) STORED;

CREATE INDEX IF NOT EXISTS idx_products_search_doc ON products USING GIN (search_doc);
CREATE INDEX IF NOT EXISTS idx_products_name_trgm  ON products USING GIN (LOWER(name) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_products_tags       ON products USING GIN (tags);

-- Published products are the only ones search ever returns.
CREATE INDEX IF NOT EXISTS idx_products_published_created
    ON products (is_published, created_at DESC);
