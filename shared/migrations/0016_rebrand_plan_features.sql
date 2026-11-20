-- GoMarketi is now Wojalo. Plan feature lists are data, shown verbatim on the
-- plan picker, so the old name ("GoMarketi checkout", "Remove GoMarketi
-- branding") has to be rewritten in the rows themselves.
--
-- Like the rest of shared/migrations, this is applied with scripts/migrate,
-- not by any service at start-up. Safe to re-run.
UPDATE plans
SET features = REPLACE(REPLACE(features::text, 'GoMarketi', 'Wojalo'), 'GoMarket', 'Wojalo')::jsonb
WHERE features::text LIKE '%GoMarket%';
