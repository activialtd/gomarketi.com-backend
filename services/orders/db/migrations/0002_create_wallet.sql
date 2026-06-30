-- Wallet balance is derived by summing transactions rather than cached,
-- so it can never drift out of sync with the ledger.
CREATE TABLE IF NOT EXISTS wallet_transactions (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id    UUID        NOT NULL,
    type        TEXT        NOT NULL CHECK (type IN ('credit','debit')),
    amount_kobo BIGINT      NOT NULL CHECK (amount_kobo > 0),
    description TEXT        NOT NULL DEFAULT '',
    reference   TEXT,
    order_id    UUID        REFERENCES orders(id) ON DELETE SET NULL,
    status      TEXT        NOT NULL DEFAULT 'completed'
                            CHECK (status IN ('pending','completed','failed')),
    -- Withdrawal destination, only set for debit transactions
    bank_name      TEXT,
    account_number TEXT,
    account_name   TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_wallet_tx_store_id ON wallet_transactions (store_id, created_at DESC);
