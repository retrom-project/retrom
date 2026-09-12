package recordstore

import (
	"context"
	"database/sql"
)

func UpdateFavoriteFolders(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"favorite_folders",
		"id,created_at_ms,name,name_key,profile_id,updated_at_ms,version",
		FavoriteFoldersUpdateRule,
	)
}

const FavoriteFoldersUpdateRule = `
WITH previous(id,created_at_ms,name,name_key,profile_id,updated_at_ms,version) AS (VALUES(?,?,?,?,?,?,?))
SELECT CASE
-- favorite_folders_guarded_update
WHEN (candidate.id<>previous.id
  OR candidate.profile_id<>previous.profile_id
  OR candidate.created_at_ms<>previous.created_at_ms
  OR candidate.version<>previous.version+1
  OR candidate.updated_at_ms<previous.updated_at_ms
  OR (candidate.name=previous.name AND candidate.name_key=previous.name_key)) THEN
'invalid favorite folder update'
ELSE '' END
FROM favorite_folders candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
