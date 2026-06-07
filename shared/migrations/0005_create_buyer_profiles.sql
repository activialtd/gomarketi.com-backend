-- 0005_create_buyer_profiles.sql
-- Created automatically (silent, no UI) on the user's first successful
-- OTP verification or OAuth login. Every authenticated user has a buyer profile.
--
-- default_address_id FK is intentionally deferred to migration 0006 to avoid
-- a circular dependency (buyer_addresses references buyer_profiles).
--
-- total_spent is stored in Kobo (BIGINT). Never use float for money.

CREATE TABLE buyer_profiles (
    id                     UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                UUID        NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    default_address_id     UUID,                -- FK added in 0006
    saved_store_ids        UUID[]      NOT NULL DEFAULT '{}',
    preferred_category_ids UUID[]      NOT NULL DEFAULT '{}',
    total_orders           INTEGER     NOT NULL DEFAULT 0,
    total_spent            BIGINT      NOT NULL DEFAULT 0,  -- Kobo; 100 Kobo = 1 Naira
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);
