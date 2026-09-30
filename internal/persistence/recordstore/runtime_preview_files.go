package recordstore

import (
	"context"
	"database/sql"

	dbapi "retrom/internal/database"
)

func CreateRuntimePreviewFiles(
	ctx context.Context, db dbapi.Executor, query string, args ...any,
) (sql.Result, error) {
	return create(
		ctx,
		db,
		query,
		args,
		"runtime_preview_files",
		"preview_session_id,role,logical_name",
		ValidateRuntimePreviewFiles,
	)
}

func ValidateRuntimePreviewFiles(ctx context.Context, db dbapi.Executor, keys ...any) error {
	return validate(ctx, db, runtime_preview_filesOwnership, keys)
}

const runtime_preview_filesOwnership = `
SELECT CASE WHEN NOT EXISTS(SELECT 1 FROM runtime_preview_sessions session
 WHERE session.id=candidate.preview_session_id AND session.state='CREATED')
THEN 'invalid runtime resource owner' ELSE '' END
FROM runtime_preview_files candidate
WHERE candidate.preview_session_id=? AND candidate.role=? AND candidate.logical_name=?`
