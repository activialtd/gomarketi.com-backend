-- 0008_create_vendor_banks.sql
-- Payout bank accounts for vendors.
-- account_number is AES-256-GCM encrypted at the application layer.
-- Only one bank account per vendor should be is_primary = true.
-- is_verified is set true after a Paystack/Flutterwave name-enquiry confirms
-- the account number matches the account name.

CREATE TABLE vendor_banks (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    vendor_profile_id UUID        NOT NULL REFERENCES vendor_profiles (id) ON DELETE CASCADE,
    bank_name         TEXT        NOT NULL,
    bank_code         TEXT        NOT NULL,   -- CBN bank code (e.g. "044" for Access Bank)
    account_number    TEXT        NOT NULL,   -- AES-256-GCM encrypted
    account_name      TEXT        NOT NULL,
    is_primary        BOOLEAN     NOT NULL DEFAULT false,
    is_verified       BOOLEAN     NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_vendor_banks_profile ON vendor_banks (vendor_profile_id);
