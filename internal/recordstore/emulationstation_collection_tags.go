package recordstore

import (
	"context"
	"database/sql"
)

func CreateEmulationstationCollectionTags(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "collection_id,tag_id", ValidateEmulationstationCollectionTags)
}

func ValidateEmulationstationCollectionTags(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, emulationstation_collection_tagsOwnership, keys)
}

const emulationstation_collection_tagsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(SELECT 1 FROM tags WHERE id=candidate.tag_id AND status='ACTIVE') OR NOT EXISTS(
    SELECT 1 FROM emulationstation_import_collections collection
    JOIN emulationstation_imports import ON import.id=collection.import_id
    WHERE collection.id=candidate.collection_id AND import.state='AWAITING_MAPPING'
  ) OR (SELECT count(*) FROM emulationstation_collection_tags relation
      JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
      WHERE relation.collection_id=candidate.collection_id)>20) THEN
'invalid active EmulationStation collection tag'
ELSE '' END
FROM emulationstation_collection_tags candidate
WHERE candidate.collection_id=? AND candidate.tag_id=?`
