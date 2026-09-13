package recordstore

import (
	"context"
	"database/sql"

	"retrom/internal/repo/dbexec"
)

func CreateGameFiles(
	ctx context.Context, db dbexec.Executor, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "game_id,role,logical_name", ValidateGameFiles)
}

func ValidateGameFiles(ctx context.Context, db dbexec.Executor, keys ...any) error {
	return validate(ctx, db, game_filesOwnership, keys)
}

const game_filesOwnership = `
SELECT CASE
WHEN (NOT EXISTS(SELECT 1 FROM games WHERE id=candidate.game_id AND status='PUBLISHED')) THEN
'game payload owner is not published'
ELSE '' END
FROM game_files candidate
WHERE candidate.game_id=? AND candidate.role=? AND candidate.logical_name=?`
