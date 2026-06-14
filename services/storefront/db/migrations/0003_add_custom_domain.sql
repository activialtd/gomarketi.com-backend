ALTER TABLE stores
    ADD COLUMN IF NOT EXISTS custom_domain        TEXT UNIQUE,
    ADD COLUMN IF NOT EXISTS custom_domain_status TEXT NOT NULL DEFAULT 'none';
-- custom_domain_status: none | pending | active | failed

CREATE INDEX IF NOT EXISTS idx_stores_custom_domain ON stores (custom_domain)
    WHERE custom_domain IS NOT NULL;
