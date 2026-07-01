-- GoMarket vendor subscription plans.
-- Prices are in Kobo (multiply by 100 for ₦ display).
-- Seeded here so IDs are stable across environments.

CREATE TABLE IF NOT EXISTS plans (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    slug            TEXT        NOT NULL UNIQUE,
    display_name    TEXT        NOT NULL,
    description     TEXT        NOT NULL DEFAULT '',
    price_kobo      BIGINT      NOT NULL DEFAULT 0,
    billing_cycle   TEXT        NOT NULL DEFAULT 'monthly'
                                CHECK (billing_cycle IN ('monthly', 'yearly', 'once')),
    product_limit   INT         NOT NULL DEFAULT 20,    -- -1 = unlimited
    store_limit     INT         NOT NULL DEFAULT 1,
    team_limit      INT         NOT NULL DEFAULT 1,
    features        JSONB       NOT NULL DEFAULT '[]',
    is_active       BOOLEAN     NOT NULL DEFAULT TRUE,
    sort_order      INT         NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed the 4 plans
INSERT INTO plans (slug, display_name, description, price_kobo, product_limit, store_limit, team_limit, features, sort_order)
VALUES
    ('free',
     'Free',
     'Perfect for testing the waters. Start selling with no upfront cost.',
     0,
     20, 1, 1,
     '["1 store","Up to 20 products","Basic analytics","GoMarketi checkout","Community support"]',
     1),

    ('starter',
     'Starter',
     'For solo sellers ready to grow. Remove limits and unlock your custom domain.',
     500000,
     200, 1, 1,
     '["1 store","Up to 200 products","Advanced analytics","Custom domain","Remove GoMarketi branding","Email support"]',
     2),

    ('growth',
     'Growth',
     'For growing brands managing multiple stores and a small team.',
     1500000,
     -1, 3, 5,
     '["3 stores","Unlimited products","Team members (up to 5)","Advanced analytics","Custom domain","Priority support","Abandoned cart recovery"]',
     3),

    ('scale',
     'Scale',
     'For established businesses that need the full platform — no limits.',
     3500000,
     -1, -1, -1,
     '["Unlimited stores","Unlimited products","Unlimited team members","White-label","Dedicated account manager","SLA support","Custom integrations"]',
     4);
