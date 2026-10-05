package recordstore

import (
	"context"
	"database/sql"

	dbapi "retrom/internal/database"
)

func CreateGameTags(ctx context.Context, db dbapi.Executor, query string, args ...any) (sql.Result, error) {
	return create(ctx, db, query, args, "game_tags", "game_id,tag_id", ValidateGameTags)
}

func ValidateGameTags(ctx context.Context, db dbapi.Executor, keys ...any) error {
	return validate(ctx, db, gameTagsOwnership, keys)
}

const gameTagsOwnership = `
SELECT CASE WHEN NOT EXISTS(SELECT 1 FROM tags WHERE id=candidate.tag_id AND status='ACTIVE')
 OR (SELECT count(*) FROM game_tags relation JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
     WHERE relation.game_id=candidate.game_id)>20 THEN 'invalid active game tag'
ELSE '' END FROM game_tags candidate WHERE candidate.game_id=? AND candidate.tag_id=?`
