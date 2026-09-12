package recordstore

import (
	"context"
	"database/sql"
)

func UpdateScrapeCandidates(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"scrape_candidates",
		"id",
		ScrapeCandidatesUpdateRule,
	)
}

const ScrapeCandidatesUpdateRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- scrape_candidates_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM scrape_candidates candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteScrapeCandidates(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"scrape_candidates",
		"id",
		ScrapeCandidatesDeleteRule,
	)
}

const ScrapeCandidatesDeleteRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- scrape_candidates_immutable_delete
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM previous`
