package recordstore

import (
	"context"
	"database/sql"
)

func CreatePegasusCollectionTags(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "collection_id,tag_id", ValidatePegasusCollectionTags)
}

func ValidatePegasusCollectionTags(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, pegasus_collection_tagsOwnership, keys)
}

const pegasus_collection_tagsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(SELECT 1 FROM tags WHERE id=candidate.tag_id AND status='ACTIVE')
  OR NOT EXISTS(
    SELECT 1 FROM pegasus_import_collections collection
    JOIN pegasus_imports import ON import.id=collection.import_id
    WHERE collection.id=candidate.collection_id AND import.state='AWAITING_MAPPING'
  )
  OR (SELECT count(*) FROM pegasus_collection_tags relation
      JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
      WHERE relation.collection_id=candidate.collection_id)>20) THEN
'invalid active Pegasus collection tag'
ELSE '' END
FROM pegasus_collection_tags candidate
WHERE candidate.collection_id=? AND candidate.tag_id=?`
