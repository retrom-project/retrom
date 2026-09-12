package recordstore

import (
	"context"
	"database/sql"
)

func CreateScrapeCandidateAssets(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateScrapeCandidateAssets)
}

func ValidateScrapeCandidateAssets(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, scrape_candidate_assetsOwnership, keys)
}

const scrape_candidate_assetsOwnership = `
SELECT CASE
WHEN (EXISTS(
  SELECT 1 FROM scrape_candidates candidate
  JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
  JOIN games game ON game.id=run.game_id
  WHERE candidate.id=candidate.scrape_candidate_id AND game.status<>'PUBLISHED'
)) THEN 'game payload owner is not published'
ELSE '' END
FROM scrape_candidate_assets candidate
WHERE candidate.id=?`
