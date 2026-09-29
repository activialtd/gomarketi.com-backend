-- Markets and vendor-defined delivery options.
--
-- markets is an admin-curated reference table: vendors pick their market from
-- this list during store setup, and buyers browse stores by market. Edit the
-- seed rows below to change what the dropdown offers.
--
-- store_delivery_options replaces the delivery zone list that was hardcoded in
-- the storefront checkout — each vendor now sets their own titles, notes and
-- prices.

CREATE TABLE IF NOT EXISTS markets (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT        NOT NULL,
    slug       TEXT        NOT NULL UNIQUE,
    city       TEXT        NOT NULL,
    state      TEXT        NOT NULL,
    is_active  BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_markets_state ON markets (state);
CREATE INDEX IF NOT EXISTS idx_markets_city  ON markets (city);

-- Seed list. ON CONFLICT keeps this migration safe to re-run and lets you add
-- rows here later without breaking existing databases.
INSERT INTO markets (name, slug, city, state) VALUES
    ('General Market',            'general-market',            'Any',           'Any'),
    ('Balogun Market',            'balogun-market',            'Lagos Island',  'Lagos'),
    ('Computer Village',          'computer-village',          'Ikeja',         'Lagos'),
    ('Alaba International Market','alaba-international-market','Ojo',           'Lagos'),
    ('Mile 12 Market',            'mile-12-market',            'Kosofe',        'Lagos'),
    ('Oyingbo Market',            'oyingbo-market',            'Ebute Metta',   'Lagos'),
    ('Ladipo Market',             'ladipo-market',             'Mushin',        'Lagos'),
    ('Tejuosho Market',           'tejuosho-market',           'Yaba',          'Lagos'),
    ('Trade Fair Complex',        'trade-fair-complex',        'Ojo',           'Lagos'),
    ('Idumota Market',            'idumota-market',            'Lagos Island',  'Lagos'),
    ('Wuse Market',               'wuse-market',               'Wuse',          'FCT'),
    ('Garki Market',              'garki-market',              'Garki',         'FCT'),
    ('Utako Market',              'utako-market',              'Utako',         'FCT'),
    ('Onitsha Main Market',       'onitsha-main-market',       'Onitsha',       'Anambra'),
    ('Ariaria International Market','ariaria-international-market','Aba',       'Abia'),
    ('Kurmi Market',              'kurmi-market',              'Kano',          'Kano'),
    ('Bodija Market',             'bodija-market',             'Ibadan',        'Oyo'),
    ('Ogbete Main Market',        'ogbete-main-market',        'Enugu',         'Enugu'),
    ('Oil Mill Market',           'oil-mill-market',           'Port Harcourt', 'Rivers')
ON CONFLICT (slug) DO NOTHING;

-- Nullable: stores created before this migration have no market, and the
-- vendor-web form can still submit without one.
ALTER TABLE stores ADD COLUMN IF NOT EXISTS market_id UUID REFERENCES markets (id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_stores_market_id ON stores (market_id);

CREATE TABLE IF NOT EXISTS store_delivery_options (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id    UUID        NOT NULL REFERENCES stores (id) ON DELETE CASCADE,
    title       TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    price_kobo  BIGINT      NOT NULL CHECK (price_kobo >= 0),
    position    INTEGER     NOT NULL DEFAULT 0,
    is_active   BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_delivery_options_store
    ON store_delivery_options (store_id, position, created_at);
