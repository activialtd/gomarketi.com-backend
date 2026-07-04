-- Per-store payment gateway configuration.
-- Merchants toggle which gateways they accept; checkout reads the enabled set.
CREATE TABLE IF NOT EXISTS store_payment_methods (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id   UUID        NOT NULL,
    gateway    TEXT        NOT NULL,   -- paystack | flutterwave | stripe | pos | manual
    enabled    BOOLEAN     NOT NULL DEFAULT false,
    config     JSONB       NOT NULL DEFAULT '{}',  -- public_key, instructions, etc.
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (store_id, gateway)
);

CREATE INDEX IF NOT EXISTS idx_payment_methods_store ON store_payment_methods (store_id);

-- Seed every store with paystack enabled by default (all others disabled)
INSERT INTO store_payment_methods (store_id, gateway, enabled)
SELECT id, 'paystack', true FROM stores
ON CONFLICT (store_id, gateway) DO NOTHING;

INSERT INTO store_payment_methods (store_id, gateway, enabled)
SELECT id, unnest(ARRAY['flutterwave','stripe','pos','manual']), false FROM stores
ON CONFLICT (store_id, gateway) DO NOTHING;
