CREATE TABLE IF NOT EXISTS vendor_subscriptions (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    vendor_profile_id    UUID        NOT NULL UNIQUE REFERENCES vendor_profiles(id) ON DELETE CASCADE,
    plan_id              UUID        NOT NULL REFERENCES plans(id),
    status               TEXT        NOT NULL DEFAULT 'active'
                                     CHECK (status IN ('active','cancelled','past_due','trialing')),
    payment_reference    TEXT,                           -- Paystack ref for paid plans
    current_period_start TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    current_period_end   TIMESTAMPTZ,                   -- NULL = free/lifetime
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_vendor_subs_vendor ON vendor_subscriptions (vendor_profile_id);
