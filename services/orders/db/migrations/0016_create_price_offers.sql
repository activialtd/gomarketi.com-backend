-- Price offers: a buyer haggling over a negotiable product.
--
-- One row is one negotiation — the buyer's opening offer, any counters back
-- and forth, and how it ended. The moves themselves are in
-- price_offer_events, so the dashboard and the buyer can read it as a
-- conversation.
--
-- status says whose turn it is:
--   pending    the buyer offered, the vendor owes a reply
--   countered  the vendor countered, the buyer owes a reply
--   accepted   a deal — the buyer can check out at agreed_kobo until expires_at
--   redeemed   the deal was paid for
--   declined / expired / withdrawn   closed without a sale
--
-- Buyers are guests on the storefront, so a guest reaches a negotiation with
-- a token rather than a user id. The token is an HMAC of the offer id, not a
-- stored secret, so every email about the offer can carry a working link.
-- Signed-in buyers are matched on email, the same way orders are.
CREATE TABLE IF NOT EXISTS price_offers (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id          UUID        NOT NULL,
    product_id        UUID        NOT NULL,
    product_name      TEXT        NOT NULL,
    product_image     TEXT,
    list_price_kobo   BIGINT      NOT NULL CHECK (list_price_kobo > 0),
    offer_kobo        BIGINT      NOT NULL CHECK (offer_kobo > 0),
    counter_kobo      BIGINT,
    agreed_kobo       BIGINT,
    quantity          INTEGER     NOT NULL DEFAULT 1 CHECK (quantity BETWEEN 1 AND 10),
    status            TEXT        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','countered','accepted','declined','expired','redeemed','withdrawn')),
    auto_accepted     BOOLEAN     NOT NULL DEFAULT FALSE,
    buyer_offers      INTEGER     NOT NULL DEFAULT 1,
    customer_name     TEXT        NOT NULL,
    customer_email    TEXT        NOT NULL,
    customer_phone    TEXT,
    message           TEXT,
    redeemed_order_id UUID,
    expires_at        TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_price_offers_store_status ON price_offers (store_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_price_offers_email        ON price_offers (LOWER(customer_email), created_at DESC);
CREATE INDEX IF NOT EXISTS idx_price_offers_open_expiry  ON price_offers (expires_at)
    WHERE status IN ('pending','countered','accepted');

-- One open negotiation per buyer per product: a buyer probing for the floor
-- with several offers at once would be told the answer by whichever one was
-- accepted instantly.
CREATE UNIQUE INDEX IF NOT EXISTS uniq_price_offers_open
    ON price_offers (product_id, LOWER(customer_email))
    WHERE status IN ('pending','countered');

CREATE TABLE IF NOT EXISTS price_offer_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    offer_id    UUID        NOT NULL REFERENCES price_offers(id) ON DELETE CASCADE,
    actor       TEXT        NOT NULL CHECK (actor IN ('buyer','vendor','system')),
    action      TEXT        NOT NULL CHECK (action IN ('offer','counter','accept','decline','withdraw','expire','redeem')),
    amount_kobo BIGINT,
    message     TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_price_offer_events_offer ON price_offer_events (offer_id, created_at);

-- The deal a line was bought at, so a negotiated sale reads as one on the
-- vendor's order and can be traced back to its negotiation.
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS offer_id UUID;
