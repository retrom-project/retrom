package recordstore

import (
	"context"
	"database/sql"

	"retrom/internal/persistence/dbexec"
)

func CreateProfiles(
	ctx context.Context, db dbexec.Executor, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateProfiles)
}

func ValidateProfiles(ctx context.Context, db dbexec.Executor, keys ...any) error {
	return validate(ctx, db, profilesOwnership, keys)
}

const profilesOwnership = `
SELECT CASE
WHEN (((SELECT state FROM instance_state WHERE id=1)='COMPLETED') AND (candidate.id='local')) THEN
'reserved profile'
ELSE '' END
FROM profiles candidate
WHERE candidate.id=?`
