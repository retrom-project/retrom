ALTER TABLE platform_instances
ADD COLUMN catalog_template_key TEXT
CHECK (
  catalog_template_key IS NULL OR (
    length(catalog_template_key) BETWEEN 3 AND 160
    AND catalog_template_key = lower(catalog_template_key)
    AND catalog_template_key NOT GLOB '*[^a-z0-9_/-]*'
    AND catalog_template_key GLOB '*/*'
    AND catalog_template_key NOT GLOB '*/*/*'
  )
);

CREATE UNIQUE INDEX platform_instances_catalog_template_key_unique
ON platform_instances(catalog_template_key)
WHERE catalog_template_key IS NOT NULL;

-- The current unreleased deployment rebuilds its database. This final fresh-
-- schema step removes historical directory seeds; there is no 039 data
-- compatibility or special startup-rejection branch.
DELETE FROM platform_instances;
