-- 0014_add_otp_purpose.sql
-- Distinguishes what an OTP session is for. 'login' covers both passwordless
-- sign-in AND post-registration email verification (both just prove control
-- of the inbox and upsert the user). 'password_reset' codes must never be
-- accepted by /v1/auth/otp/verify — only by the password reset endpoint.

ALTER TABLE otp_sessions
    ADD COLUMN purpose TEXT NOT NULL DEFAULT 'login'
    CHECK (purpose IN ('login', 'password_reset'));
