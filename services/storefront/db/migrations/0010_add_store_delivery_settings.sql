-- 0010_add_store_delivery_settings.sql
-- Delivery fee was a flat, hardcoded frontend constant (₦1,500, free above
-- ₦50,000) duplicated across every storefront theme, with no way for a
-- vendor to change it. Defaults here match those existing hardcoded values
-- exactly, so no vendor's storefront pricing changes until they touch the
-- new setting from their dashboard.
ALTER TABLE stores
    ADD COLUMN delivery_fee_kobo             BIGINT NOT NULL DEFAULT 150000,
    ADD COLUMN free_delivery_threshold_kobo  BIGINT NOT NULL DEFAULT 5000000;
