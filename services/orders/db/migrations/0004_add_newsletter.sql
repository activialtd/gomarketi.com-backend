-- Newsletter subscribers — customers who opted in from a store's storefront
CREATE TABLE IF NOT EXISTS newsletter_subscribers (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id        UUID        NOT NULL,
    email           TEXT        NOT NULL,
    name            TEXT        NOT NULL DEFAULT '',
    subscribed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    unsubscribed_at TIMESTAMPTZ,
    UNIQUE (store_id, email)
);

CREATE INDEX IF NOT EXISTS idx_newsletter_store ON newsletter_subscribers (store_id);

-- Email campaigns composed and sent by the merchant
CREATE TABLE IF NOT EXISTS email_campaigns (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id         UUID        NOT NULL,
    subject          TEXT        NOT NULL,
    body_html        TEXT        NOT NULL,
    status           TEXT        NOT NULL DEFAULT 'draft'
                                  CHECK (status IN ('draft','sending','sent','failed')),
    recipients_count INT         NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at          TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_campaigns_store ON email_campaigns (store_id, created_at DESC);
