package recordstore

import (
	"context"
	"database/sql"

	"retrom/internal/persistence/dbexec"
)

func UpdateUploadSessions(
	ctx context.Context, db dbexec.Executor, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"upload_sessions",
		"id,purpose",
		UploadSessionsUpdateRule,
	)
}

const UploadSessionsUpdateRule = `
WITH previous(id,purpose) AS (VALUES(?,?))
SELECT CASE
-- upload_sessions_purpose_immutable
WHEN ((candidate.purpose IS NOT previous.purpose) AND (1=1)) THEN 'upload purpose is immutable'
ELSE '' END
FROM upload_sessions candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
