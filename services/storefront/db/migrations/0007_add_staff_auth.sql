-- Add password_hash and is_active to store_staff for staff login.
-- Expand role enum to include granular RBAC roles.

ALTER TABLE store_staff
  ADD COLUMN IF NOT EXISTS password_hash TEXT,
  ADD COLUMN IF NOT EXISTS is_active     BOOLEAN     NOT NULL DEFAULT TRUE,
  ADD COLUMN IF NOT EXISTS created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Replace the old role constraint with the expanded set.
ALTER TABLE store_staff DROP CONSTRAINT IF EXISTS store_staff_role_check;
ALTER TABLE store_staff ADD CONSTRAINT store_staff_role_check
  CHECK (role IN ('manager', 'fulfillment', 'support', 'analytics_only', 'staff'));

-- Index for login lookup and uniqueness per store.
CREATE INDEX IF NOT EXISTS idx_store_staff_email ON store_staff (email);
CREATE UNIQUE INDEX IF NOT EXISTS idx_store_staff_store_email ON store_staff (store_id, email);
