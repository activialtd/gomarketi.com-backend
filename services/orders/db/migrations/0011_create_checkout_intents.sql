-- Record what the buyer is paying for, before they pay for it.
--
-- Checkout is pay-then-create: the browser charges Paystack and only then
-- asks us to save the order. If anything interrupts that second step — the
-- buyer's network dropping at exactly the wrong moment, the tab closing, the
-- phone sleeping — the money is taken and no order exists. Recovery was a
-- copy of the payload in that buyer's own localStorage, retried only if they
-- happened to reopen the same checkout in the same browser. If they never
-- came back, nobody ever found out: not the buyer, not the vendor, not us.
--
-- Writing the intended order here first means the server can finish the job
-- without the browser. Paystack's charge.success webhook (and the sweep that
-- backs it up) looks the reference up here and creates the order itself.
--
-- The row is the plan, not the money: it is written before any charge and
-- means nothing on its own. Only a verified Paystack charge against the same
-- reference turns it into an order, which is why storing it needs no auth.
CREATE TABLE IF NOT EXISTS checkout_intents (
    payment_reference TEXT        PRIMARY KEY,
    -- 'order' for a single store, 'checkout' for a multi-vendor basket —
    -- decides which service call replays it.
    kind              TEXT        NOT NULL,
    payload           JSONB       NOT NULL,
    -- Set once the order(s) exist, whichever path got there first.
    fulfilled_at      TIMESTAMPTZ,
    attempts          INTEGER     NOT NULL DEFAULT 0,
    last_error        TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The sweep's query: unfulfilled, oldest first.
CREATE INDEX IF NOT EXISTS idx_checkout_intents_unfulfilled
    ON checkout_intents (created_at)
    WHERE fulfilled_at IS NULL;
