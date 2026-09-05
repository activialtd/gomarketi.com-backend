-- 0009_add_wallet_reference_uniqueness.sql
-- Enables idempotent crediting from the new Paystack DVA-deposit webhook —
-- Paystack retries webhook deliveries on anything but a fast 200, so the
-- handler needs a cheap "have I already processed this transaction
-- reference" check. A plain UNIQUE constraint gives that for free (Postgres
-- treats multiple NULLs as distinct, so existing rows with no reference at
-- all — most order-credit rows — are unaffected).

ALTER TABLE wallet_transactions
    ADD CONSTRAINT wallet_transactions_reference_key UNIQUE (reference);
