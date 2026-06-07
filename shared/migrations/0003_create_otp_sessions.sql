-- 0003_create_otp_sessions.sql
-- Stores active email OTP verification sessions.
-- otp_hash stores the bcrypt hash of the 6-digit OTP — never the raw OTP.
-- session_token is an HMAC-signed opaque token returned to the client.
-- attempts is incremented BEFORE the bcrypt comparison to prevent timing attacks.

CREATE TABLE otp_sessions (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT        NOT NULL,
    session_token TEXT        NOT NULL UNIQUE,
    otp_hash      TEXT        NOT NULL,   -- bcrypt hash of the 6-digit OTP
    attempts      INTEGER     NOT NULL DEFAULT 0,
    expires_at    TIMESTAMPTZ NOT NULL,   -- 10 minutes from creation
    used_at       TIMESTAMPTZ,            -- set when OTP is verified; NULL = unused
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_otp_sessions_email   ON otp_sessions (email);
-- Used by a periodic cleanup job to purge expired sessions.
CREATE INDEX idx_otp_sessions_expires ON otp_sessions (expires_at);
