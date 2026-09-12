package recordstore

import (
	"context"
	"database/sql"
)

func UpdateContentIdentityClaims(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"content_identity_claims",
		"platform_id,content_identity_digest",
		ContentIdentityClaimsUpdateRule,
	)
}

const ContentIdentityClaimsUpdateRule = `
WITH previous(platform_id,content_identity_digest) AS (VALUES(?,?))
SELECT CASE
-- content_identity_claims_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM content_identity_claims candidate CROSS JOIN previous
WHERE candidate.platform_id=previous.platform_id AND
candidate.content_identity_digest=previous.content_identity_digest`

func DeleteContentIdentityClaims(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"content_identity_claims",
		"platform_id,content_identity_digest",
		ContentIdentityClaimsDeleteRule,
	)
}

const ContentIdentityClaimsDeleteRule = `
WITH previous(platform_id,content_identity_digest) AS (VALUES(?,?))
SELECT CASE
-- content_identity_claims_immutable_delete
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM previous`
