package metadatascrape

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
)

type initialReviewMetadata struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Developer   string `json:"developer"`
	Publisher   string `json:"publisher"`
	Genre       string `json:"genre"`
	Players     *int   `json:"players"`
	ReleaseYear *int   `json:"releaseYear"`
}

func mergeInitialReviewMetadata(currentJSON, candidateJSON string) (string, string, error) {
	var current, candidate initialReviewMetadata
	if err := json.Unmarshal([]byte(currentJSON), &current); err != nil {
		return "", "", fmt.Errorf("decode initial review metadata: %w", err)
	}
	if err := json.Unmarshal([]byte(candidateJSON), &candidate); err != nil {
		return "", "", fmt.Errorf("decode scrape candidate metadata: %w", err)
	}
	if strings.TrimSpace(candidate.Title) != "" {
		current.Title = candidate.Title
	}
	if strings.TrimSpace(candidate.Description) != "" {
		current.Description = candidate.Description
	}
	if strings.TrimSpace(candidate.Developer) != "" {
		current.Developer = candidate.Developer
	}
	if strings.TrimSpace(candidate.Publisher) != "" {
		current.Publisher = candidate.Publisher
	}
	if strings.TrimSpace(candidate.Genre) != "" {
		current.Genre = candidate.Genre
	}
	if candidate.Players != nil {
		current.Players = candidate.Players
	}
	if candidate.ReleaseYear != nil {
		current.ReleaseYear = candidate.ReleaseYear
	}
	merged, err := json.Marshal(current)
	if err != nil {
		return "", "", fmt.Errorf("encode initial review metadata: %w", err)
	}
	return string(merged), current.Title, nil
}

func firstReadyCandidateAsset(
	ctx context.Context,
	transaction *sql.Tx,
	candidateID, kind string,
) (sql.NullString, error) {
	var assetID sql.NullString
	err := transaction.QueryRowContext(ctx, `
SELECT id
FROM scrape_candidate_assets
WHERE scrape_candidate_id=?
AND kind_hint=?
AND status='READY'
ORDER BY ordinal,
id
LIMIT 1
`, candidateID, kind).Scan(&assetID)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.NullString{}, nil
	}
	if err != nil {
		return sql.NullString{}, fmt.Errorf("select initial review asset: %w", err)
	}
	return assetID, nil
}

type initialImportScope struct {
	itemID      string
	importJobID string
}

func loadInitialImportScope(
	ctx context.Context,
	transaction *sql.Tx,
	runID string,
) (initialImportScope, bool, error) {
	var scope initialImportScope
	var itemState string
	err := transaction.QueryRowContext(ctx, `
SELECT i.id,
i.import_job_id,
i.state
FROM metadata_scrape_runs r
JOIN import_items i ON i.id=r.import_item_id
WHERE r.id=?
`, runID).Scan(&scope.itemID, &scope.importJobID, &itemState)
	if errors.Is(err, sql.ErrNoRows) {
		return initialImportScope{}, false, nil
	}
	if err != nil {
		return initialImportScope{}, false, fmt.Errorf("load initial import scrape: %w", err)
	}
	if itemState != "SCRAPING" {
		return initialImportScope{}, false, nil
	}
	return scope, true, nil
}

func applyInitialReviewScreenshots(
	ctx context.Context,
	transaction *sql.Tx,
	draftID, candidateID string,
	now int64,
) error {
	screenshotRows, err := transaction.QueryContext(ctx, `
SELECT id,
ordinal
FROM scrape_candidate_assets
WHERE scrape_candidate_id=?
AND kind_hint='SCREENSHOT'
AND status='READY'
ORDER BY ordinal,
id
`, candidateID)
	if err != nil {
		return fmt.Errorf("select initial review screenshots: %w", err)
	}
	defer func() { cleanup.Error("close", screenshotRows.Close()) }()
	for screenshotRows.Next() {
		var assetID string
		var ordinal int
		if err := screenshotRows.Scan(&assetID, &ordinal); err != nil {
			return fmt.Errorf("scan initial review screenshot: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_draft_screenshot_assets(review_draft_id,
ordinal,
candidate_asset_id,
created_at_ms) VALUES(?,
?,
?,
?)
`, draftID, ordinal, assetID, now); err != nil {
			return fmt.Errorf("apply initial review screenshot: %w", err)
		}
	}
	if err := screenshotRows.Err(); err != nil {
		return fmt.Errorf("scan initial review screenshots: %w", err)
	}
	return nil
}

func applyInitialReviewCandidate(
	ctx context.Context,
	transaction *sql.Tx,
	runID, itemID string,
	now int64,
) error {
	var candidateID, candidateMetadata string
	err := transaction.QueryRowContext(ctx, `
SELECT c.id,
c.normalized_metadata_json
FROM scrape_candidates c
WHERE c.scrape_run_id=?
ORDER BY (SELECT count(*)
FROM scrape_candidate_hits h
WHERE h.scrape_candidate_id=c.id) DESC,
COALESCE((SELECT min(e.query_order)
FROM scrape_candidate_hits h
JOIN metadata_scrape_query_attempts q ON q.id=h.query_attempt_id
JOIN content_hash_evidence e ON e.id=q.content_hash_evidence_id
WHERE h.scrape_candidate_id=c.id), 2147483647),
c.provider_game_id,
c.id
LIMIT 1
`, runID).Scan(&candidateID, &candidateMetadata)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("select initial scrape candidate: %w", err)
	}

	var draftID, currentMetadata string
	if err := transaction.QueryRowContext(ctx, `
SELECT id,
metadata_json
FROM review_drafts
WHERE import_item_id=?
`, itemID).Scan(&draftID, &currentMetadata); err != nil {
		return fmt.Errorf("load initial review draft: %w", err)
	}
	mergedMetadata, title, err := mergeInitialReviewMetadata(currentMetadata, candidateMetadata)
	if err != nil {
		return err
	}
	coverID, err := firstReadyCandidateAsset(ctx, transaction, candidateID, "COVER")
	if err != nil {
		return err
	}
	backgroundID, err := firstReadyCandidateAsset(ctx, transaction, candidateID, "BACKGROUND")
	if err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE review_drafts
SET selected_candidate_id=?,
cover_candidate_asset_id=?,
background_candidate_asset_id=?,
metadata_json=?,
updated_at_ms=?
WHERE id=?
`, candidateID, nullableText(coverID), nullableText(backgroundID), mergedMetadata, now, draftID); err != nil {
		return fmt.Errorf("apply initial scrape candidate: %w", err)
	}
	if err := applyInitialReviewScreenshots(ctx, transaction, draftID, candidateID, now); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_items
SET search_text=trim(search_text || ' ' || lower(?))
WHERE id=?
`, title, itemID); err != nil {
		return fmt.Errorf("update initial review search text: %w", err)
	}
	return nil
}

// completeInitialImport atomically exposes a newly imported item only after its first scrape has settled.
func (service *Service) completeInitialImport(
	ctx context.Context,
	transaction *sql.Tx,
	runID string,
	now int64,
) error {
	scope, active, err := loadInitialImportScope(ctx, transaction, runID)
	if err != nil {
		return err
	}
	if !active {
		return nil
	}
	if err := applyInitialReviewCandidate(ctx, transaction, runID, scope.itemID, now); err != nil {
		return err
	}

	result, err := transaction.ExecContext(ctx, `
UPDATE import_items
SET state='REVIEW_PENDING',
failed_stage=NULL,
last_error_code=NULL,
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='SCRAPING'
	`, now, scope.itemID)
	if err != nil {
		return fmt.Errorf("expose initial review item: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("expose initial review item: %w", errInitialItemState)
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE import_jobs
SET running_item_count=running_item_count-1,
review_pending_item_count=review_pending_item_count+1,
state=CASE
WHEN running_item_count=1 AND (failed_item_count>0 OR rejected_file_count>0) THEN 'PARTIAL_FAILURE'
WHEN running_item_count=1 THEN 'REVIEW_PENDING'
ELSE 'RUNNING'
END,
version=version+1,
updated_at_ms=?
WHERE id=?
AND running_item_count>0
	`, now, scope.importJobID)
	if err != nil {
		return fmt.Errorf("update initial import progress: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("update initial import progress: %w", errInitialProgressState)
	}
	return nil
}

func (service *Service) complete(ctx context.Context, runID, jobID string, candidateCount int) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	if err := service.completeInitialImport(ctx, transaction, runID, now); err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE metadata_scrape_runs
SET state='COMPLETED',
version=version+1,
updated_at_ms=?,
completed_at_ms=?
WHERE id=?
AND state='RUNNING'
`, now, now, runID); err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='SUCCEEDED',
finished_at_ms=?,
leased_until_ms=NULL,
heartbeat_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
`, now, now, now, jobID); err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	data := fmt.Sprintf(`{"candidateCount":%d}`, candidateCount)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) SELECT id,
scope_type,
scope_id,
'SUCCEEDED',
?,
?
FROM jobs
WHERE id=?
`, data, now, jobID); err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit metadata asset fetch: %w", err)
	}
	return nil
}

func failInitialImport(
	ctx context.Context,
	transaction *sql.Tx,
	runID, code string,
	now int64,
) error {
	scope, active, err := loadInitialImportScope(ctx, transaction, runID)
	if err != nil {
		return err
	}
	if !active {
		return nil
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE import_items
SET state='FAILED_RETRYABLE',
failed_stage='SCRAPING',
last_error_code=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='SCRAPING'
`, code, now, scope.itemID)
	if err != nil {
		return fmt.Errorf("fail initial import item: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("fail initial import item: %w", errInitialItemState)
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE import_jobs
SET running_item_count=running_item_count-1,
failed_item_count=failed_item_count+1,
state=CASE WHEN running_item_count=1 THEN 'PARTIAL_FAILURE' ELSE 'RUNNING' END,
last_error_code=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND running_item_count>0
`, code, now, scope.importJobID)
	if err != nil {
		return fmt.Errorf("fail initial import progress: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("fail initial import progress: %w", errInitialProgressState)
	}
	return nil
}

func (service *Service) fail(ctx context.Context, runID, jobID, code string, cause error) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: %w (persist failure: %w)", code, cause, err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(
		ctx,
		`
UPDATE metadata_scrape_runs
SET state='FAILED',
error_code=?,
version=version+1,
updated_at_ms=?,
completed_at_ms=?
WHERE id=?
AND state='RUNNING'
`,
		code,
		now,
		now,
		runID,
	); err != nil {
		return fmt.Errorf("%s: %w (persist failure: %w)", code, cause, err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`
UPDATE jobs
SET state='FAILED',
error_code=?,
error_retryable=1,
finished_at_ms=?,
leased_until_ms=NULL,
version=version+1,
updated_at_ms=?
WHERE id=?
`,
		code,
		now,
		now,
		jobID,
	); err != nil {
		return fmt.Errorf("%s: %w (persist failure: %w)", code, cause, err)
	}
	if err := failInitialImport(ctx, transaction, runID, code, now); err != nil {
		return fmt.Errorf("%s: %w (persist failure: %w)", code, cause, err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) SELECT id,
scope_type,
scope_id,
'FAILED',
?,
?
FROM jobs
WHERE id=?
`,
		fmt.Sprintf(`{"code":%q}`, code),
		now,
		jobID,
	); err != nil {
		return fmt.Errorf("%s: %w (persist failure: %w)", code, cause, err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("%s: %w (persist failure: %w)", code, cause, err)
	}
	return fmt.Errorf("%s: %w", code, cause)
}

func nullableStatus(status int) any {
	if status == 0 {
		return nil
	}
	return status
}

func newID() string {
	value, _ := uuid.NewV7()
	return value.String()
}
