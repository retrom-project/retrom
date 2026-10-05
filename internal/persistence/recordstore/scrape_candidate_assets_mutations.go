package recordstore

import (
	"context"
	"database/sql"

	dbapi "retrom/internal/database"
)

func UpdateScrapeCandidateAssets(
	ctx context.Context, db dbapi.Executor, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"scrape_candidate_assets",
		"id,file_record",
		ScrapeCandidateAssetsUpdateRule,
	)
}

const ScrapeCandidateAssetsUpdateRule = `
WITH previous(id,file_record)
AS (VALUES(?::text,?::text))
SELECT CASE
-- scrape_candidate_assets_published_update
WHEN ((candidate.file_record IS DISTINCT FROM previous.file_record) AND (candidate.file_record IS NOT NULL AND EXISTS(
  SELECT 1 FROM scrape_candidates scrape_candidate
  JOIN metadata_scrape_runs run ON run.id=scrape_candidate.scrape_run_id
  JOIN games game ON game.id=run.game_id
  WHERE scrape_candidate.id=candidate.scrape_candidate_id AND game.status<>'PUBLISHED'
))) THEN 'game payload owner is not published'
ELSE '' END
FROM scrape_candidate_assets candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
