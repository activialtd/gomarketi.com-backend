-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (
    user_id, token_hash, family_id, device_id, user_agent, ip_address, expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6::inet, $7
)
RETURNING id, user_id, token_hash, family_id, device_id, user_agent,
          ip_address::text, expires_at, revoked_at, created_at;

-- name: GetRefreshTokenByHash :one
SELECT id, user_id, token_hash, family_id, device_id, user_agent,
       ip_address::text, expires_at, revoked_at, created_at
FROM refresh_tokens
WHERE token_hash = $1;

-- name: GetTokensByFamily :many
SELECT id, user_id, token_hash, family_id, device_id, user_agent,
       ip_address::text, expires_at, revoked_at, created_at
FROM refresh_tokens
WHERE family_id = $1;

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens
SET revoked_at = now()
WHERE id = $1;

-- RevokeTokenFamily revokes every active token in a family — used when
-- token reuse is detected (a revoked token is presented again).
-- name: RevokeTokenFamily :exec
UPDATE refresh_tokens
SET revoked_at = now()
WHERE family_id = $1
  AND revoked_at IS NULL;
