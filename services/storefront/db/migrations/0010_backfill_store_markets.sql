-- Backfill market_id for stores created before markets existed.
--
-- Stores are matched to a market by city and state, but only where that city
-- has exactly one market — picking arbitrarily between, say, Balogun and
-- Idumota for a store that just says "Lagos Island" would invent an
-- association the vendor never made. Everything else falls back to General
-- Market, which is accurate (the store is somewhere) and leaves the vendor
-- free to choose properly in their dashboard.

-- Guarded on the columns this migration needs, because it collides with
-- staging's 0009_create_markets.sql: on a database where that landed first,
-- `markets` has neither `is_active` nor `slug`, and every statement below
-- would abort,
-- taking storefront's startup with it. 0012 reconciles the two schemas and
-- redoes this work, so skipping here loses nothing.
DO $guard$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'markets' AND column_name = 'is_active')
       AND EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_name = 'markets' AND column_name = 'slug') THEN
        WITH unambiguous AS (
            -- HAVING COUNT(*) = 1 means array_agg holds exactly one id; Postgres has
            -- no MIN() for uuid, so take that single element.
            SELECT LOWER(city) AS city, LOWER(state) AS state, (array_agg(id))[1] AS market_id
            FROM markets
            WHERE is_active = TRUE AND LOWER(state) <> 'any'
            GROUP BY LOWER(city), LOWER(state)
            HAVING COUNT(*) = 1
        )
        UPDATE stores s
        SET market_id = u.market_id
        FROM unambiguous u
        WHERE s.market_id IS NULL
          AND s.city IS NOT NULL AND s.state IS NOT NULL
          AND LOWER(s.city) = u.city
          AND LOWER(s.state) = u.state;

        UPDATE stores
        SET market_id = (SELECT id FROM markets WHERE slug = 'general-market')
        WHERE market_id IS NULL;
    END IF;
END
$guard$;
