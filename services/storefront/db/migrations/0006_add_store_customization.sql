-- Storefront customization: branding, social presence, and theme.
-- logo_url and tagline already exist from 0001_create_stores.sql
-- theme_config already exists from 0004_add_theme_config.sql
ALTER TABLE stores ADD COLUMN IF NOT EXISTS hero_image_url TEXT;
ALTER TABLE stores ADD COLUMN IF NOT EXISTS site_description TEXT;
ALTER TABLE stores ADD COLUMN IF NOT EXISTS social_links JSONB NOT NULL DEFAULT '{}';

-- theme_config JSON structure (for reference):
-- {
--   "primary_color": "#1A7A42",
--   "secondary_color": "#0A2E1A",
--   "accent_color": "#F0FAF3",
--   "background_color": "#ffffff",
--   "text_color": "#1C1C1C",
--   "font_heading": "Inter",
--   "font_body": "Inter",
--   "hero_style": "full",
--   "products_per_row": 3,
--   "button_style": "rounded",
--   "show_hero": true,
--   "show_featured": true,
--   "show_categories": true
-- }
