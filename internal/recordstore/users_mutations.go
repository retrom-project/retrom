package recordstore

import (
	"context"
	"database/sql"
)

func UpdateUsers(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"users",
		"id,created_at_ms,profile_id,role,status,username",
		UsersUpdateRule,
	)
}

const UsersUpdateRule = `
WITH previous(id,created_at_ms,profile_id,role,status,username) AS (VALUES(?,?,?,?,?,?))
SELECT CASE
-- users_deleted_terminal
WHEN ((candidate.status IS NOT previous.status) AND (previous.status='DELETED' AND
candidate.status!='DELETED')) THEN 'deleted user is terminal'
-- users_identity_immutable
WHEN ((candidate.profile_id IS NOT previous.profile_id OR candidate.username IS NOT previous.username OR
candidate.created_at_ms IS NOT previous.created_at_ms) AND (1=1)) THEN 'immutable user identity'
-- users_last_enabled_admin
WHEN ((candidate.role IS NOT previous.role OR candidate.status IS NOT previous.status) AND
((previous.role='ADMIN' AND previous.status='ENABLED' AND
     (candidate.role!='ADMIN' OR candidate.status!='ENABLED')) AND (NOT EXISTS (
    SELECT 1 FROM users
    WHERE id!=previous.id AND role='ADMIN' AND status='ENABLED'
  )))) THEN 'last enabled admin'
ELSE '' END
FROM users candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteUsers(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"users",
		"id",
		UsersDeleteRule,
	)
}

const UsersDeleteRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- users_no_physical_delete
WHEN (1=1) THEN 'users are soft deleted'
ELSE '' END
FROM previous`
