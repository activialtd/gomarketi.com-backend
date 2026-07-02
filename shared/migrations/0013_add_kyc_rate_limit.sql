-- Rate-limiting columns for KYC submissions.
-- Tracks attempt count and last attempt time per vendor profile to enforce
-- the max 3-attempt-per-24-hour rule required by CBN AML guidelines.
ALTER TABLE vendor_profiles
  ADD COLUMN IF NOT EXISTS kyc_attempts        INTEGER     NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS kyc_last_attempt_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS kyc_smile_job_id    TEXT;       -- Smile ID audit trail reference
