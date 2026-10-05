package recordstore

import (
	"context"
	"database/sql"

	dbapi "retrom/internal/database"
)

func UpdateUsers(
	ctx context.Context, db dbapi.Executor, change Update,
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
WITH previous(id,created_at_ms,profile_id,role,status,username)
AS (VALUES(?::text,?::bigint,?::text,?::text,?::text,?::text))
SELECT CASE
-- users_deleted_terminal
WHEN ((candidate.status IS DISTINCT FROM previous.status) AND (previous.status='DELETED' AND
candidate.status!='DELETED')) THEN 'deleted user is terminal'
-- users_identity_immutable
WHEN ((candidate.profile_id IS DISTINCT FROM previous.profile_id OR candidate.username IS DISTINCT FROM
 previous.username OR
candidate.created_at_ms IS DISTINCT FROM previous.created_at_ms) AND (1=1)) THEN 'immutable user identity'
-- users_last_enabled_admin
WHEN ((candidate.role IS DISTINCT FROM previous.role OR candidate.status IS DISTINCT FROM previous.status) AND
((previous.role='ADMIN' AND previous.status='ENABLED' AND
     (candidate.role!='ADMIN' OR candidate.status!='ENABLED')) AND (NOT EXISTS (
    SELECT 1 FROM users
    WHERE id!=previous.id AND role='ADMIN' AND status='ENABLED'
  )))) THEN 'last enabled admin'
ELSE '' END
FROM users candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteUsers(
	ctx context.Context, db dbapi.Executor, scope Scope,
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
WITH previous(id)
AS (VALUES(?::text))
SELECT CASE
-- users_no_physical_delete
WHEN (1=1) THEN 'users are soft deleted'
ELSE '' END
FROM previous`
