package recordstore

import (
	"context"
	"database/sql"

	dbapi "retrom/internal/database"
)

func CreateGameFiles(
	ctx context.Context, db dbapi.Executor, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "game_files", "game_id,role,logical_name", ValidateGameFiles)
}

func ValidateGameFiles(ctx context.Context, db dbapi.Executor, keys ...any) error {
	return validate(ctx, db, game_filesOwnership, keys)
}

const game_filesOwnership = `
SELECT CASE
WHEN NOT json_valid(candidate.file_record) THEN 'invalid game file record'
WHEN json_extract(candidate.file_record,'$.path') NOT LIKE
 'files/' || substr(candidate.game_id,-2) || '/' || candidate.game_id || '/%'
 THEN 'game file is outside its game directory'
WHEN (NOT EXISTS(SELECT 1 FROM games WHERE id=candidate.game_id AND status='PUBLISHED')) THEN
'game payload owner is not published'
ELSE '' END
FROM game_files candidate
WHERE candidate.game_id=? AND candidate.role=? AND candidate.logical_name=?`
