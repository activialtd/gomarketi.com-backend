-- 0006_create_buyer_addresses.sql
-- Saved delivery addresses for buyers.
-- full_address is AES-256-GCM encrypted at the application layer before storage.
-- coordinates uses PostGIS GEOMETRY(POINT, 4326) — WGS-84, same as GPS/H3.

CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE buyer_addresses (
    id               UUID                  PRIMARY KEY DEFAULT gen_random_uuid(),
    buyer_profile_id UUID                  NOT NULL REFERENCES buyer_profiles (id) ON DELETE CASCADE,
    label            TEXT                  NOT NULL,   -- e.g. "Home", "Office"
    full_address     TEXT                  NOT NULL,   -- AES-256-GCM encrypted
    city             TEXT                  NOT NULL,
    state            TEXT                  NOT NULL,
    coordinates      GEOMETRY(POINT, 4326),            -- nullable until geocoded
    is_default       BOOLEAN               NOT NULL DEFAULT false,
    created_at       TIMESTAMPTZ           NOT NULL DEFAULT now()
);

CREATE INDEX idx_buyer_addresses_profile     ON buyer_addresses (buyer_profile_id);
CREATE INDEX idx_buyer_addresses_coordinates ON buyer_addresses USING GIST (coordinates);

-- Close the circular FK now that buyer_addresses exists.
ALTER TABLE buyer_profiles
    ADD CONSTRAINT fk_buyer_profiles_default_address
    FOREIGN KEY (default_address_id) REFERENCES buyer_addresses (id) ON DELETE SET NULL;
