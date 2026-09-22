package recordstore

import (
	"context"
	"database/sql"

	"retrom/internal/dbexec"
)

func CreateSourceCollectionTags(
	ctx context.Context, db dbexec.Executor, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "collection_id,tag_id", ValidateSourceCollectionTags)
}

func ValidateSourceCollectionTags(ctx context.Context, db dbexec.Executor, keys ...any) error {
	return validate(ctx, db, source_collection_tagsOwnership, keys)
}

const source_collection_tagsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(SELECT 1 FROM tags WHERE id=candidate.tag_id AND status='ACTIVE')
  OR NOT EXISTS(
    SELECT 1 FROM source_import_collections collection
    JOIN source_imports import ON import.id=collection.import_id
    WHERE collection.id=candidate.collection_id AND import.state='AWAITING_MAPPING'
  )
  OR (SELECT count(*) FROM source_collection_tags relation
      JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
      WHERE relation.collection_id=candidate.collection_id)>20) THEN
'invalid active Source collection tag'
ELSE '' END
FROM source_collection_tags candidate
WHERE candidate.collection_id=? AND candidate.tag_id=?`
