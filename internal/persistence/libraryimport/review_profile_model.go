package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/dbexec"
	"retrom/internal/profilemodel"
)

func readRPGReviewProfile(
	ctx context.Context, executor dbexec.Executor, itemID string,
) (*profilemodel.RPGReview, error) {
	var raw string
	if err := executor.QueryRowContext(ctx, `
SELECT review_profile_json FROM import_items WHERE id=? AND review_profile_json IS NOT NULL`, itemID,
	).Scan(&raw); err != nil {
		return nil, fmt.Errorf("read review profile: %w", err)
	}
	decoded, err := profilemodel.Decode(profilemodel.Review, raw)
	if err != nil {
		return nil, fmt.Errorf("decode review profile: %w", err)
	}
	profile, ok := decoded.(*profilemodel.RPGReview)
	if !ok {
		return nil, fmt.Errorf("%w: review model %T", profilemodel.ErrInvalidModel, decoded)
	}
	return profile, nil
}
