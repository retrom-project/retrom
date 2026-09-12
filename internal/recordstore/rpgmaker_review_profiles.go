package recordstore

import (
	"context"
	"database/sql"
)

func CreateRpgmakerReviewProfiles(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "review_draft_id", ValidateRpgmakerReviewProfiles)
}

func ValidateRpgmakerReviewProfiles(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, rpgmaker_review_profilesOwnership, keys)
}

const rpgmaker_review_profilesOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=candidate.provider_id AND target.target_id=candidate.target_id
)) THEN 'invalid runtime target snapshot'
ELSE '' END
FROM rpgmaker_review_profiles candidate
WHERE candidate.review_draft_id=?`
