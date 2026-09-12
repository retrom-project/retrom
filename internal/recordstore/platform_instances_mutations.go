package recordstore

import (
	"context"
	"database/sql"
)

func UpdatePlatformInstances(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"platform_instances",
		"id,default_core_id",
		PlatformInstancesUpdateRule,
	)
}

const PlatformInstancesUpdateRule = `
WITH previous(id,default_core_id) AS (VALUES(?,?))
SELECT CASE
-- platform_instances_enabled_default_update
WHEN ((candidate.default_core_id IS NOT previous.default_core_id) AND ((NOT EXISTS (
    SELECT 1 FROM platform_cores
    WHERE platform_id = candidate.platform_id AND core_id = candidate.default_core_id AND enabled = 1
  )))) THEN 'platform default core is not enabled'
ELSE '' END
FROM platform_instances candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
