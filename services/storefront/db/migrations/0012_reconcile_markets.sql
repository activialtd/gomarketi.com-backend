-- Reconcile the two competing definitions of `markets` left by the staging
-- merge, and restore the seed rows that went missing with them.
--
-- 0008_add_markets_and_delivery_options.sql created markets with slug +
-- is_active and seeded the launch list. Staging's 0009_create_markets.sql
-- created the same table with aliases + coordinates and no seed at all.
-- Whichever ran first won, and the loser's INSERT/columns never landed — so
-- a database built the staging way has the table but none of the markets in
-- it (no Balogun, no Computer Village, nothing), while the code needs
-- columns from both sides: matchMarket() selects `aliases`, ListMarkets()
-- and 0011_curate_markets.sql select `slug` and `is_active`.
--
-- This converges any database to the union of both definitions, re-seeds the
-- full list, and re-applies the launch curation. Every statement is
-- idempotent, so it is safe on a database that already had it right.

CREATE TABLE IF NOT EXISTS markets (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT        NOT NULL,
    city       TEXT        NOT NULL,
    state      TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE markets ADD COLUMN IF NOT EXISTS slug      TEXT;
ALTER TABLE markets ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE markets ADD COLUMN IF NOT EXISTS aliases   TEXT[] NOT NULL DEFAULT '{}';

-- Rows created by the staging definition have no slug — derive one from the
-- name so the seed below can match on it.
UPDATE markets
SET slug = trim(BOTH '-' FROM lower(regexp_replace(name, '[^a-zA-Z0-9]+', '-', 'g')))
WHERE slug IS NULL OR slug = '';

-- The staging table is unique on (name, state), so two states could hold the
-- same market name and collapse to one slug. Suffix the later ones, else the
-- unique index below cannot be built.
WITH dup AS (
    SELECT id, row_number() OVER (PARTITION BY slug ORDER BY created_at, id) AS rn
    FROM markets
)
UPDATE markets m
SET slug = m.slug || '-' || dup.rn
FROM dup
WHERE dup.id = m.id AND dup.rn > 1;

ALTER TABLE markets ALTER COLUMN slug SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS markets_slug_key ON markets (slug);

-- Re-seed. Bare ON CONFLICT DO NOTHING (no target) is deliberate: it absorbs
-- a clash on slug *or* on the staging table's UNIQUE (name, state), so this
-- runs cleanly whichever constraints the database happens to carry.
INSERT INTO markets (name, slug, city, state, aliases) VALUES
    ('General Market',              'general-market',              'Any',           'Any',     '{}'),
    ('Balogun Market',              'balogun-market',              'Lagos Island',  'Lagos',   '{balogun}'),
    ('Computer Village',            'computer-village',            'Ikeja',         'Lagos',   '{cv,"computer village ikeja"}'),
    ('Alaba International Market',  'alaba-international-market',  'Ojo',           'Lagos',   '{alaba}'),
    ('Mile 12 Market',              'mile-12-market',              'Kosofe',        'Lagos',   '{"mile 12","mile twelve"}'),
    ('Oyingbo Market',              'oyingbo-market',              'Ebute Metta',   'Lagos',   '{oyingbo}'),
    ('Ladipo Market',               'ladipo-market',               'Mushin',        'Lagos',   '{ladipo}'),
    ('Tejuosho Market',             'tejuosho-market',             'Yaba',          'Lagos',   '{tejuosho}'),
    ('Lagos International Trade Fair','trade-fair-complex',         'Ojo',           'Lagos',   '{"trade fair","international trade fair"}'),
    ('Idumota Market',              'idumota-market',              'Lagos Island',  'Lagos',   '{idumota}'),
    ('Agege Market',                'agege-market',                'Agege',         'Lagos',   '{agege}'),
    ('Wuse Market',                 'wuse-market',                 'Wuse',          'FCT',     '{wuse}'),
    ('Garki Market',                'garki-market',                'Garki',         'FCT',     '{garki}'),
    ('Utako Market',                'utako-market',                'Utako',         'FCT',     '{utako}'),
    ('Onitsha Main Market',         'onitsha-main-market',         'Onitsha',       'Anambra', '{onitsha,"main market"}'),
    ('Ariaria International Market','ariaria-international-market','Aba',           'Abia',    '{ariaria}'),
    ('Kurmi Market',                'kurmi-market',                'Kano',          'Kano',    '{kurmi}'),
    ('Bodija Market',               'bodija-market',               'Ibadan',        'Oyo',     '{bodija}'),
    ('Ogbete Main Market',          'ogbete-main-market',          'Enugu',         'Enugu',   '{ogbete}'),
    ('Oil Mill Market',             'oil-mill-market',             'Port Harcourt', 'Rivers',  '{"oil mill"}')
ON CONFLICT DO NOTHING;

-- Give aliases to rows that predate this migration (seeded by hand or by
-- scripts/seed-marketplace) without clobbering any already set.
UPDATE markets m SET aliases = v.aliases
FROM (VALUES
    ('balogun-market',              '{balogun}'::TEXT[]),
    ('computer-village',            '{cv,"computer village ikeja"}'::TEXT[]),
    ('alaba-international-market',  '{alaba}'::TEXT[]),
    ('mile-12-market',              '{"mile 12","mile twelve"}'::TEXT[]),
    ('oyingbo-market',              '{oyingbo}'::TEXT[]),
    ('ladipo-market',               '{ladipo}'::TEXT[]),
    ('tejuosho-market',             '{tejuosho}'::TEXT[]),
    ('trade-fair-complex',          '{"trade fair","international trade fair"}'::TEXT[]),
    ('idumota-market',              '{idumota}'::TEXT[]),
    ('agege-market',                '{agege}'::TEXT[]),
    ('onitsha-main-market',         '{onitsha,"main market"}'::TEXT[])
) AS v(slug, aliases)
WHERE m.slug = v.slug AND m.aliases = '{}';

-- Re-apply the launch curation from 0011, which could not have run on a
-- database where is_active did not exist until a moment ago.
UPDATE markets SET is_active = (slug IN (
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
));

-- No store may point at a market buyers cannot browse.
UPDATE stores SET market_id = (SELECT id FROM markets WHERE slug = 'general-market')
WHERE market_id IN (SELECT id FROM markets WHERE is_active = FALSE);
