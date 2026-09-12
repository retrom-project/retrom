package recordstore

import (
	"context"
	"database/sql"
)

func UpdateMetadataScrapeQueryAttempts(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"metadata_scrape_query_attempts",
		"id",
		MetadataScrapeQueryAttemptsUpdateRule,
	)
}

const MetadataScrapeQueryAttemptsUpdateRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- scrape_attempts_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM metadata_scrape_query_attempts candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteMetadataScrapeQueryAttempts(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"metadata_scrape_query_attempts",
		"id",
		MetadataScrapeQueryAttemptsDeleteRule,
	)
}

const MetadataScrapeQueryAttemptsDeleteRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- scrape_attempts_immutable_delete
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM previous`
