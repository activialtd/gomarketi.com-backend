-- 0001_create_users.sql
-- Core identity record. Auth-method agnostic — one row per person.
-- email and phone are nullable to support OAuth-only accounts (no email/phone
-- required at signup). Both have partial unique indexes to allow multiple NULLs.

CREATE TYPE how_heard_source AS ENUM (
    'instagram',
    'twitter_x',
    'tiktok',
    'facebook',
    'friend_referral',
    'google_search',
    'youtube',
    'tv_ad',
    'radio',
    'influencer',
    'store_flyer',
    'other'
);

CREATE TABLE users (
    id                UUID             PRIMARY KEY DEFAULT gen_random_uuid(),
    email             TEXT             UNIQUE,
    full_name         TEXT,
    avatar_url        TEXT,
    phone             TEXT             UNIQUE,
    is_email_verified BOOLEAN          NOT NULL DEFAULT false,
    is_phone_verified BOOLEAN          NOT NULL DEFAULT false,
    is_active         BOOLEAN          NOT NULL DEFAULT true,
    profile_completed BOOLEAN          NOT NULL DEFAULT false,
    how_heard         how_heard_source,
    how_heard_other   TEXT,
    -- terms_accepted_at stores the timestamp of acceptance for NDPR compliance.
    -- A boolean would not be sufficient — we must record when consent was given.
    terms_accepted_at TIMESTAMPTZ,
    marketing_consent BOOLEAN          NOT NULL DEFAULT false,
    last_login_at     TIMESTAMPTZ,
    created_at        TIMESTAMPTZ      NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ      NOT NULL DEFAULT now()
);

-- Partial indexes: allow multiple NULL emails/phones, enforce uniqueness only
-- when the value is present.
CREATE UNIQUE INDEX idx_users_email ON users (email) WHERE email IS NOT NULL;
CREATE UNIQUE INDEX idx_users_phone ON users (phone) WHERE phone IS NOT NULL;
