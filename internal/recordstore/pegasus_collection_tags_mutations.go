package recordstore

import (
	"context"
	"database/sql"
)

func UpdatePegasusCollectionTags(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"pegasus_collection_tags",
		"collection_id,tag_id",
		PegasusCollectionTagsUpdateRule,
	)
}

const PegasusCollectionTagsUpdateRule = `
WITH previous(collection_id,tag_id) AS (VALUES(?,?))
SELECT CASE
-- pegasus_collection_tags_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM pegasus_collection_tags candidate CROSS JOIN previous
WHERE candidate.collection_id=previous.collection_id AND candidate.tag_id=previous.tag_id`

func DeletePegasusCollectionTags(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"pegasus_collection_tags",
		"collection_id,tag_id",
		PegasusCollectionTagsDeleteRule,
	)
}

const PegasusCollectionTagsDeleteRule = `
WITH previous(collection_id,tag_id) AS (VALUES(?,?))
SELECT CASE
-- pegasus_collection_tags_validate_delete
WHEN (EXISTS(SELECT 1 FROM tags WHERE id=previous.tag_id AND status='ACTIVE')
  AND NOT EXISTS(
    SELECT 1 FROM pegasus_import_collections collection
    JOIN pegasus_imports import ON import.id=collection.import_id
    WHERE collection.id=previous.collection_id AND import.state='AWAITING_MAPPING'
  )) THEN 'Pegasus collection tag mapping is frozen'
ELSE '' END
FROM previous`
