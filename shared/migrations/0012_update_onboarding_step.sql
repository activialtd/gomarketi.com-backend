-- Add plan_selected step between account_created and business_details.
-- ALTER TYPE ... ADD VALUE is safe — no views or constraints need updating.
ALTER TYPE onboarding_step ADD VALUE IF NOT EXISTS 'plan_selected' AFTER 'account_created';
