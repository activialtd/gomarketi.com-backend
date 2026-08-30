-- 0015_add_vendor_paystack_account.sql
-- Auto-provisioned Paystack Customer + Dedicated Virtual Account (DVA) per
-- vendor, created best-effort right after plan selection. Nullable — DVA
-- creation is async and can fail without blocking onboarding.

ALTER TABLE vendor_profiles
    ADD COLUMN paystack_customer_code      TEXT,
    ADD COLUMN paystack_dva_account_number TEXT,
    ADD COLUMN paystack_dva_bank_name      TEXT,
    ADD COLUMN paystack_dva_account_name   TEXT;

-- Free plan product cap raised from the original 20 to 30.
UPDATE plans SET product_limit = 30 WHERE slug = 'free';
