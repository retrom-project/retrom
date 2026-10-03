package libraryimport

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
)

// ReviewDraftAssets contains the storage predicates used while applying a
// draft patch. The caller owns the transaction so validation and the eventual
// draft update observe one snapshot.
type ReviewDraftAssets struct{ executor dbapi.Executor }

func BindReviewDraftAssets(executor dbapi.Executor) ReviewDraftAssets {
	return ReviewDraftAssets{executor: executor}
}

func (records ReviewDraftAssets) ValidCandidate(
	ctx context.Context, itemID, assetID string,
) (bool, error) {
	var count int
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT count(*)
FROM scrape_candidate_assets a
JOIN scrape_candidates c ON c.id=a.scrape_candidate_id
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
WHERE a.id=? AND r.import_item_id=? AND r.state='COMPLETED' AND a.status='READY'
`, assetID, itemID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("query review candidate asset: %w", err)
	}
	return count == 1, nil
}

func (records ReviewDraftAssets) ValidUploaded(
	ctx context.Context, itemID, assetID, kind string,
) (bool, error) {
	var count int
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT count(*) FROM review_uploaded_assets
WHERE id=? AND import_item_id=? AND kind=?
`, assetID, itemID, kind).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("query review uploaded asset: %w", err)
	}
	return count == 1, nil
}
