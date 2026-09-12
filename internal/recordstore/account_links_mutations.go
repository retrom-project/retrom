package recordstore

import (
	"context"
	"database/sql"
)

func UpdateAccountLinks(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"account_links",
		"id,consumed_at_ms,revoked_at_ms",
		AccountLinksUpdateRule,
	)
}

const AccountLinksUpdateRule = `
WITH previous(id,consumed_at_ms,revoked_at_ms) AS (VALUES(?,?,?))
SELECT CASE
-- account_links_terminal_immutable
WHEN (previous.consumed_at_ms IS NOT NULL OR previous.revoked_at_ms IS NOT NULL) THEN
'terminal account link'
ELSE '' END
FROM account_links candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteAccountLinks(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"account_links",
		"id",
		AccountLinksDeleteRule,
	)
}

const AccountLinksDeleteRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- account_links_no_delete
WHEN (1=1) THEN 'account links are retained'
ELSE '' END
FROM previous`
