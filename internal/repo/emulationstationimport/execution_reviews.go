package emulationstationimport

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/recordstore"
	application "retrom/internal/service/emulationstationimport"
)

func (records executionRecords) Reviews(
	ctx context.Context,
	id string,
	limit int,
) ([]application.ExecutionReview, error) {
	rows, err := records.executor.QueryContext(ctx, executionReviewSQL+`
WHERE source.import_id=? AND (source.execution_state IN ('PENDING','COPYING','VALIDATING')
OR source.retryable=1 AND source.execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED'))
AND item.state='REVIEW_PENDING' AND item.review_handoff_kind='EMULATIONSTATION'
ORDER BY source.id,item.id LIMIT ?`, id, limit)
	if err != nil {
		return nil, fmt.Errorf("query interrupted EmulationStation reviews: %w", err)
	}
	defer func() { cleanup.Error("close interrupted EmulationStation reviews", rows.Close()) }()
	result := []application.ExecutionReview{}
	for rows.Next() {
		value, err := scanExecutionReview(rows)
		if err != nil {
			return nil, fmt.Errorf("read interrupted EmulationStation review: %w", err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate interrupted EmulationStation reviews: %w", err)
	}
	return result, nil
}

func (records executionRecords) Fence(ctx context.Context, before application.LeaseSnapshot, now int64) error {
	return records.fence(ctx, application.ExecutionFinish{Before: before, NowMS: now})
}

func (records executionRecords) CompleteReview(
	ctx context.Context,
	change application.ExecutionReviewCompletion,
) error {
	if err := records.Fence(ctx, change.Before, change.NowMS); err != nil {
		return err
	}
	return records.completeReviewProjection(ctx, change)
}

func (records executionRecords) completeReviewProjection(
	ctx context.Context, change application.ExecutionReviewCompletion,
) error {
	item := change.Review
	for _, state := range change.Preparation {
		if err := records.reviewState(ctx, item, state, change.NowMS); err != nil {
			return err
		}
		item.State, item.Version, item.Retryable = state, item.Version+1, false
	}
	result, err := recordstore.UpdateEmulationstationImportItems(ctx, records.executor, recordstore.Update{
		Set: `execution_state='REVIEW_PENDING',library_import_job_id=?,library_import_item_id=?,
warnings_json=?,error_code=NULL,error_details_json=NULL,retryable=0,completed_at_ms=?,
updated_at_ms=?,version=version+1`,
		Values: []any{item.ReservedJobID, item.ReservedItemID, change.WarningsJSON, change.NowMS, change.NowMS},
		Scope: recordstore.Scope{
			Where: `id=? AND import_id=? AND version=? AND execution_state='VALIDATING'
AND library_import_job_id IS ? AND library_import_item_id IS ? AND metadata_json=? AND warnings_json=?
AND EXISTS(SELECT 1 FROM import_items WHERE id=? AND import_job_id=? AND state='REVIEW_PENDING')`,
			Args: []any{
				item.ItemID, change.Before.ImportID, item.Version, optionalText(item.LibraryJobID),
				optionalText(item.LibraryItemID), item.MetadataJSON, item.WarningsJSON, item.ReservedItemID, item.ReservedJobID,
			},
		},
	})
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	return records.reviewProgress(ctx, change)
}

func (records executionRecords) reviewState(
	ctx context.Context,
	item application.ExecutionReview,
	state string,
	now int64,
) error {
	result, err := recordstore.UpdateEmulationstationImportItems(ctx, records.executor, recordstore.Update{
		Set: `execution_state=?,version=version+1,updated_at_ms=?,
error_code=NULL,error_details_json=NULL,retryable=0,completed_at_ms=NULL`, Values: []any{state, now},
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND execution_state=? AND retryable=?`,
			Args:  []any{item.ItemID, item.Version, item.State, item.Retryable},
		},
	})
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}

func (records executionRecords) reviewProgress(
	ctx context.Context,
	change application.ExecutionReviewCompletion,
) error {
	counts, err := LoadTerminalItemCounts(ctx, records.executor, change.Before.ImportID)
	if err != nil {
		return err
	}
	values := terminalCountValues(counts)
	values = append(values, change.NowMS)
	result, err := recordstore.UpdateEmulationstationImports(ctx, records.executor, recordstore.Update{
		Set: `skipped_mapping_item_count=?,review_pending_item_count=?,published_item_count=?,review_discarded_item_count=?,
existing_item_count=?,blocked_item_count=?,failed_item_count=?,cancelled_item_count=?,
version=version+1,updated_at_ms=?`,
		Values: values, Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state=?`,
			Args:  []any{change.Before.ImportID, change.Before.ImportVersion, change.Before.ImportState},
		},
	})
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	result, err = records.executor.ExecContext(ctx, `INSERT INTO job_events
(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'EMULATIONSTATION_IMPORT',?,'PROGRESS',
json_object('schemaVersion',1,'itemId',?,'outcome','REVIEW_PENDING'),?)`,
		change.Before.JobID, change.Before.ImportID, change.Review.ItemID, change.NowMS)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}
