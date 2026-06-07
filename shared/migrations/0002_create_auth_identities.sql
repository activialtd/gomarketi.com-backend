-- 0002_create_auth_identities.sql
-- One row per (user, auth method). A user can have email + google + apple
-- simultaneously. access_token and refresh_token are AES-256-GCM encrypted
-- before storage — never stored as plaintext.

CREATE TYPE auth_provider AS ENUM ('email', 'google', 'apple');

CREATE TABLE auth_identities (
    id                  UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID          NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider            auth_provider NOT NULL,
    provider_uid        TEXT          NOT NULL,
    provider_email      TEXT,
    provider_name       TEXT,
    provider_avatar     TEXT,
    access_token        TEXT,                    -- AES-256-GCM encrypted
    refresh_token       TEXT,                    -- AES-256-GCM encrypted
    token_expires_at    TIMESTAMPTZ,
    -- Apple sends the user's name only on the very first sign-in. This flag
    -- prevents overwriting a captured name with an empty value on return visits.
    apple_name_captured BOOLEAN       NOT NULL DEFAULT false,
    is_primary          BOOLEAN       NOT NULL DEFAULT false,
    last_used_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ   NOT NULL DEFAULT now(),

    -- Each (provider, provider_uid) pair must be globally unique.
    CONSTRAINT uq_provider_uid  UNIQUE (provider, provider_uid),
    -- A user can only have one identity per provider.
    CONSTRAINT uq_user_provider UNIQUE (user_id, provider)
);

CREATE INDEX idx_auth_identities_user_id ON auth_identities (user_id);
