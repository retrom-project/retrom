package recordstore

import (
	"context"
	"database/sql"
)

func UpdatePlatformCores(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"platform_cores",
		"platform_id,core_id,enabled",
		PlatformCoresUpdateRule,
	)
}

const PlatformCoresUpdateRule = `
WITH previous(platform_id,core_id,enabled) AS (VALUES(?,?,?))
SELECT CASE
-- platform_cores_in_use_disable
WHEN ((candidate.enabled IS NOT previous.enabled) AND ((candidate.enabled = 0) AND (EXISTS (
    SELECT 1 FROM platform_instances WHERE platform_id = previous.platform_id AND default_core_id =
previous.core_id AND deleted_at_ms IS NULL
  )))) THEN 'platform core is in use'
ELSE '' END
FROM platform_cores candidate CROSS JOIN previous
WHERE candidate.platform_id=previous.platform_id AND candidate.core_id=previous.core_id`
