-- 0001_create_admin_users.sql
-- Platform-level staff accounts for the GoMarketi Admin Center — distinct
-- from store_staff (per-store vendor staff, owned by the identity service).
-- Role gating happens in application code (see src/auth/middleware.ts),
-- not Postgres row-level security, matching identity's ValidRoles pattern.

CREATE TABLE IF NOT EXISTS admin_users (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email          TEXT        NOT NULL UNIQUE,
    full_name      TEXT        NOT NULL,
    password_hash  TEXT        NOT NULL,
    role           TEXT        NOT NULL
                               CHECK (role IN ('agent', 'supervisor', 'super_admin')),
    is_active      BOOLEAN     NOT NULL DEFAULT TRUE,
    last_login_at  TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
