-- 0004_create_refresh_tokens.sql
-- One row per active session across all auth methods.
-- token_hash is SHA-256(raw_token) — the raw token is delivered to the client
-- via HttpOnly cookie and never stored here.
--
-- family_id groups tokens issued from the same initial login. If we detect a
-- reuse attempt (revoked token used again within the same family), we revoke
-- every token in that family immediately — token theft response.

CREATE TABLE refresh_tokens (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash TEXT        NOT NULL UNIQUE,  -- SHA-256 of the raw token
    family_id  UUID        NOT NULL,         -- groups tokens from one login lineage
    device_id  TEXT,
    user_agent TEXT,
    ip_address INET,
    expires_at TIMESTAMPTZ NOT NULL,         -- 30 days from issuance
    revoked_at TIMESTAMPTZ,                  -- NULL = active
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_refresh_tokens_user_id   ON refresh_tokens (user_id);
CREATE INDEX idx_refresh_tokens_family_id ON refresh_tokens (family_id);
-- Partial index: only active tokens need fast expiry lookups.
CREATE INDEX idx_refresh_tokens_expires   ON refresh_tokens (expires_at)
    WHERE revoked_at IS NULL;
