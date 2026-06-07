-- name: GetIdentityByProviderUID :one
SELECT id, user_id, provider, provider_uid, provider_email,
       provider_name, provider_avatar, access_token, refresh_token,
       token_expires_at, apple_name_captured, is_primary,
       last_used_at, created_at, updated_at
FROM auth_identities
WHERE provider = $1 AND provider_uid = $2;

-- name: GetIdentityByUserAndProvider :one
SELECT id, user_id, provider, provider_uid, provider_email,
       provider_name, provider_avatar, access_token, refresh_token,
       token_expires_at, apple_name_captured, is_primary,
       last_used_at, created_at, updated_at
FROM auth_identities
WHERE user_id = $1 AND provider = $2;

-- UpsertEmailIdentity is used on every successful OTP verification.
-- provider_uid = the email address (unique per email provider).
-- name: UpsertEmailIdentity :one
INSERT INTO auth_identities (user_id, provider, provider_uid, is_primary)
VALUES ($1, 'email', $2, true)
ON CONFLICT (user_id, provider) DO UPDATE
SET last_used_at = now(),
    updated_at   = now()
RETURNING id, user_id, provider, provider_uid, provider_email,
          provider_name, provider_avatar, access_token, refresh_token,
          token_expires_at, apple_name_captured, is_primary,
          last_used_at, created_at, updated_at;

-- CreateOAuthIdentity inserts a new Google or Apple identity row.
-- access_token and refresh_token are AES-256-GCM encrypted before being passed here.
-- name: CreateOAuthIdentity :one
INSERT INTO auth_identities (
    user_id, provider, provider_uid, provider_email, provider_name,
    provider_avatar, access_token, refresh_token, token_expires_at,
    apple_name_captured, is_primary
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
RETURNING id, user_id, provider, provider_uid, provider_email,
          provider_name, provider_avatar, access_token, refresh_token,
          token_expires_at, apple_name_captured, is_primary,
          last_used_at, created_at, updated_at;

-- name: UpdateOAuthTokens :exec
UPDATE auth_identities
SET access_token     = $2,
    refresh_token    = $3,
    token_expires_at = $4,
    last_used_at     = now(),
    updated_at       = now()
WHERE id = $1;

-- name: UpdateIdentityLastUsed :exec
UPDATE auth_identities
SET last_used_at = now(),
    updated_at   = now()
WHERE id = $1;
