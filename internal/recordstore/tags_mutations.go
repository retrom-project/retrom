package recordstore

import (
	"context"
	"database/sql"
)

func UpdateTags(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"tags",
		"id,created_at_ms,created_by_user_id,status,updated_at_ms,version",
		TagsUpdateRule,
	)
}

const TagsUpdateRule = `
WITH previous(id,created_at_ms,created_by_user_id,status,updated_at_ms,version) AS (VALUES(?,?,?,?,?,?))
SELECT CASE
-- tags_guarded_update
WHEN (candidate.id<>previous.id
  OR candidate.created_by_user_id<>previous.created_by_user_id
  OR candidate.created_at_ms<>previous.created_at_ms
  OR candidate.version<>previous.version+1
  OR candidate.updated_at_ms<previous.updated_at_ms
  OR previous.status='DELETED'
  OR (previous.status='ACTIVE' AND candidate.status NOT IN ('ACTIVE','DELETED'))
  OR (candidate.status='ACTIVE' AND candidate.deleted_at_ms IS NOT NULL)
  OR (candidate.status='DELETED' AND candidate.deleted_at_ms IS NULL)) THEN 'invalid tag update'
ELSE '' END
FROM tags candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteTags(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"tags",
		"id",
		TagsDeleteRule,
	)
}

const TagsDeleteRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- tags_no_delete
WHEN (1=1) THEN 'tag tombstones are immutable'
ELSE '' END
FROM previous`
