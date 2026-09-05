-- The live database's free-plan product_limit was manually bumped to 30 at
-- some point after the original seed (0010_create_plans.sql seeded 20)
-- without updating the matching "Up to 20 products" marketing copy — the
-- enforced limit (catalogue's CreateProduct) and the displayed copy had
-- drifted apart. Formalizes 30 as the real, intended value and fixes the
-- copy to match what's actually enforced.
--
-- Note: shared/migrations is not auto-applied by any service's embedded
-- migration runner (each service only embeds its own local migrations/
-- directory) — this file is documentation of a manually-applied change,
-- same as 0010_create_plans.sql before it.
UPDATE plans
SET product_limit = 30,
    features = '["1 store","Up to 30 products","Basic analytics","GoMarketi checkout","Community support"]'
WHERE slug = 'free';
