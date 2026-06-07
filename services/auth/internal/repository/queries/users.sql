-- name: GetUserByID :one
SELECT id, email, full_name, avatar_url, phone,
       is_email_verified, is_phone_verified, is_active,
       profile_completed, how_heard, how_heard_other,
       terms_accepted_at, marketing_consent, last_login_at,
       created_at, updated_at
FROM users
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT id, email, full_name, avatar_url, phone,
       is_email_verified, is_phone_verified, is_active,
       profile_completed, how_heard, how_heard_other,
       terms_accepted_at, marketing_consent, last_login_at,
       created_at, updated_at
FROM users
WHERE email = $1;

-- UpsertUserByEmail is used by all three auth methods.
-- ON CONFLICT preserves existing full_name/avatar if already set
-- (prevents a second login from overwriting user-edited data).
-- is_email_verified is OR'd so a verified flag is never downgraded.
-- name: UpsertUserByEmail :one
INSERT INTO users (email, full_name, avatar_url, is_email_verified)
VALUES ($1, $2, $3, $4)
ON CONFLICT (email) WHERE email IS NOT NULL
DO UPDATE SET
    last_login_at     = now(),
    updated_at        = now(),
    full_name         = COALESCE(users.full_name,  EXCLUDED.full_name),
    avatar_url        = COALESCE(users.avatar_url, EXCLUDED.avatar_url),
    is_email_verified = users.is_email_verified OR EXCLUDED.is_email_verified
RETURNING id, email, full_name, avatar_url, phone,
          is_email_verified, is_phone_verified, is_active,
          profile_completed, how_heard, how_heard_other,
          terms_accepted_at, marketing_consent, last_login_at,
          created_at, updated_at;

-- name: CreateUserWithPassword :one
INSERT INTO users (email, full_name, password_hash, terms_accepted_at, marketing_consent)
VALUES ($1, $2, $3, CASE WHEN $4 THEN now() ELSE NULL END, $5)
RETURNING id, email, full_name, avatar_url, phone,
          is_email_verified, is_phone_verified, is_active,
          profile_completed, how_heard, how_heard_other,
          terms_accepted_at, marketing_consent, last_login_at,
          created_at, updated_at;

-- name: UpdateLastLogin :exec
UPDATE users
SET last_login_at = now(),
    updated_at    = now()
WHERE id = $1;
