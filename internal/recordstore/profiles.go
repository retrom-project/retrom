package recordstore

import (
	"context"
	"database/sql"
)

func CreateProfiles(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateProfiles)
}

func ValidateProfiles(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, profilesOwnership, keys)
}

const profilesOwnership = `
SELECT CASE
WHEN (((SELECT state FROM instance_state WHERE id=1)='COMPLETED') AND (candidate.id='local')) THEN
'reserved profile'
ELSE '' END
FROM profiles candidate
WHERE candidate.id=?`
