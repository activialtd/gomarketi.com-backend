-- Backfill market_id for stores created before markets existed.
--
-- Stores are matched to a market by city and state, but only where that city
-- has exactly one market — picking arbitrarily between, say, Balogun and
-- Idumota for a store that just says "Lagos Island" would invent an
-- association the vendor never made. Everything else falls back to General
-- Market, which is accurate (the store is somewhere) and leaves the vendor
-- free to choose properly in their dashboard.

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
