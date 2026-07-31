-- 0008_add_store_location.sql
-- Adds geolocation to stores so the consumer app can find vendors near a
-- buyer. Same PostGIS point pattern as buyer_addresses
-- (shared/migrations/0006_create_buyer_addresses.sql): nullable POINT,
-- GIST index, ST_MakePoint/ST_X/ST_Y at the query layer.

CREATE EXTENSION IF NOT EXISTS postgis;

ALTER TABLE stores ADD COLUMN coordinates GEOMETRY(POINT, 4326);

CREATE INDEX idx_stores_coordinates ON stores USING GIST (coordinates);
