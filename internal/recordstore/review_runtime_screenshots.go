package recordstore

import (
	"context"
	"database/sql"
)

func CreateReviewRuntimeScreenshots(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateReviewRuntimeScreenshots)
}

func ValidateReviewRuntimeScreenshots(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, review_runtime_screenshotsOwnership, keys)
}

const review_runtime_screenshotsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=candidate.provider_id AND target.target_id=candidate.target_id
)) THEN 'invalid runtime target snapshot'
ELSE '' END
FROM review_runtime_screenshots candidate
WHERE candidate.id=?`
