-- 0007_create_vendor_profiles.sql
-- Created when a user explicitly starts vendor onboarding (not automatic).
-- Sensitive fields (bvn, nin, id_number) are AES-256-GCM encrypted at the
-- application layer before storage — never stored as plaintext.
-- onboarding_step is resumable: the client reads it and routes to the
-- correct screen on re-entry.

CREATE TYPE business_type AS ENUM (
    'sole_trader',
    'partnership',
    'limited_company',
    'ngo'
);

CREATE TYPE kyc_status AS ENUM (
    'none',
    'pending',
    'verified',
    'rejected'
);

CREATE TYPE onboarding_step AS ENUM (
    'account_created',
    'business_details',
    'store_profile',
    'location_set',
    'kyc_submitted',
    'completed'
);

CREATE TABLE vendor_profiles (
    id               UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID            NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    business_name    TEXT,
    business_type    business_type,
    employee_range   TEXT,
    year_established INTEGER,
    social_url       TEXT,
    bvn              TEXT,                       -- AES-256-GCM encrypted
    nin              TEXT,                       -- AES-256-GCM encrypted
    tin              TEXT,
    cac_number       TEXT,
    cac_document_url TEXT,
    id_type          TEXT,
    id_number        TEXT,                       -- AES-256-GCM encrypted
    id_document_url  TEXT,
    selfie_url       TEXT,
    kyc_status       kyc_status      NOT NULL DEFAULT 'none',
    onboarding_step  onboarding_step NOT NULL DEFAULT 'account_created',
    is_active        BOOLEAN         NOT NULL DEFAULT false,
    referral_code    TEXT            UNIQUE,
    created_at       TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ     NOT NULL DEFAULT now()
);

CREATE INDEX idx_vendor_profiles_referral_code
    ON vendor_profiles (referral_code) WHERE referral_code IS NOT NULL;
