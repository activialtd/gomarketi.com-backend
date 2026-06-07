-- name: CreateOTPSession :one
INSERT INTO otp_sessions (email, session_token, otp_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING id, email, session_token, otp_hash, attempts, expires_at, used_at, created_at;

-- name: GetOTPSessionByToken :one
SELECT id, email, session_token, otp_hash, attempts, expires_at, used_at, created_at
FROM otp_sessions
WHERE session_token = $1;

-- Increment attempts BEFORE running bcrypt.Compare so the counter is updated
-- even if the process crashes mid-verify. This prevents timing attacks where
-- an attacker retries at the exact moment of a crash to avoid the counter.
-- name: IncrementOTPAttempts :one
UPDATE otp_sessions
SET attempts = attempts + 1
WHERE id = $1
RETURNING id, email, session_token, otp_hash, attempts, expires_at, used_at, created_at;

-- name: MarkOTPSessionUsed :exec
UPDATE otp_sessions
SET used_at = now()
WHERE id = $1;
