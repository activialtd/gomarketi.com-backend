-- Curate the market list down to the ten that matter for launch.
--
-- Migration 0008 seeded a wider spread across Nigeria. Markets outside the
-- launch set are deactivated rather than deleted: a store may already point
-- at one, and a deleted row would break that reference. Deactivated markets
-- disappear from the dropdown and from search, and can be switched back on
-- with a single UPDATE when you expand.
--
-- 'General Market' stays active alongside the ten as the fallback for a
-- vendor whose market is not listed — it is what the backfill assigns and
-- what the store-setup form expects to always be there.

-- Agege was missing from the original seed.
INSERT INTO markets (name, slug, city, state) VALUES
    ('Agege Market', 'agege-market', 'Agege', 'Lagos')
ON CONFLICT (slug) DO NOTHING;

-- Trade Fair goes by its full name in the dropdown.
UPDATE markets SET name = 'Lagos International Trade Fair'
WHERE slug = 'trade-fair-complex';

-- The launch ten, plus the General Market fallback. Everything else is
-- switched off.
UPDATE markets SET is_active = FALSE
WHERE slug NOT IN (
    'general-market',
    'agege-market',
    'balogun-market',
    'trade-fair-complex',
    'computer-village',
    'alaba-international-market',
    'mile-12-market',
    'ladipo-market',
    'oyingbo-market',
    'tejuosho-market',
    'onitsha-main-market'
);

UPDATE markets SET is_active = TRUE
WHERE slug IN (
    'general-market',
    'agege-market',
    'balogun-market',
    'trade-fair-complex',
    'computer-village',
    'alaba-international-market',
    'mile-12-market',
    'ladipo-market',
    'oyingbo-market',
    'tejuosho-market',
    'onitsha-main-market'
);

-- Any store pointing at a market that just went inactive falls back to
-- General Market, so no store is left in a market buyers cannot browse.
UPDATE stores SET market_id = (SELECT id FROM markets WHERE slug = 'general-market')
WHERE market_id IN (SELECT id FROM markets WHERE is_active = FALSE);
