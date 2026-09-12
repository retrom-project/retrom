package recordstore

import (
	"context"
	"database/sql"
)

func CreatePlatformInstances(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidatePlatformInstances)
}

func ValidatePlatformInstances(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, platform_instancesOwnership, keys)
}

const platform_instancesOwnership = `
SELECT CASE
WHEN ((NOT EXISTS (
    SELECT 1 FROM platform_cores
    WHERE platform_id = candidate.platform_id AND core_id = candidate.default_core_id AND enabled = 1
  ))) THEN 'platform default core is not enabled'
ELSE '' END
FROM platform_instances candidate
WHERE candidate.id=?`
