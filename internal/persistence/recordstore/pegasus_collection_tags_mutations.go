package recordstore

import (
	"context"
	"database/sql"

	"retrom/internal/dbexec"
)

func UpdateSourceCollectionTags(
	ctx context.Context, db dbexec.Executor, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"source_collection_tags",
		"collection_id,tag_id",
		SourceCollectionTagsUpdateRule,
	)
}

const SourceCollectionTagsUpdateRule = `
WITH previous(collection_id,tag_id) AS (VALUES(?,?))
SELECT CASE
-- source_collection_tags_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM source_collection_tags candidate CROSS JOIN previous
WHERE candidate.collection_id=previous.collection_id AND candidate.tag_id=previous.tag_id`

func DeleteSourceCollectionTags(
	ctx context.Context, db dbexec.Executor, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"source_collection_tags",
		"collection_id,tag_id",
		SourceCollectionTagsDeleteRule,
	)
}

const SourceCollectionTagsDeleteRule = `
WITH previous(collection_id,tag_id) AS (VALUES(?,?))
SELECT CASE
-- source_collection_tags_validate_delete
WHEN (EXISTS(SELECT 1 FROM tags WHERE id=previous.tag_id AND status='ACTIVE')
  AND NOT EXISTS(
    SELECT 1 FROM source_import_collections collection
    JOIN source_imports import ON import.id=collection.import_id
    WHERE collection.id=previous.collection_id AND import.state='AWAITING_MAPPING'
  )) THEN 'Source collection tag mapping is frozen'
ELSE '' END
FROM previous`
