package emulationstationimport

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/emulationstationimport"
)

func (records executionRecords) Reviews(
	ctx context.Context,
	id string,
	limit int,
) ([]application.ExecutionReview, error) {
	rows, err := records.executor.QueryContext(ctx, `SELECT source.id,source.execution_state,source.version,
COALESCE(source.library_import_job_id,''),COALESCE(source.library_import_item_id,''),
source.metadata_json,source.warnings_json,item.import_job_id,item.id,
(SELECT count(*) FROM import_items sibling WHERE sibling.import_job_id=item.import_job_id)
FROM emulationstation_import_items source
JOIN server_import_upload_owners owner ON owner.kind='EMULATIONSTATION' AND owner.source_item_id=source.id
JOIN import_jobs ordinary ON ordinary.upload_session_id=owner.upload_session_id
JOIN import_items item ON item.import_job_id=ordinary.id
WHERE source.import_id=? AND source.execution_state IN ('PENDING','COPYING','VALIDATING')
AND item.state='REVIEW_PENDING' AND item.review_handoff_kind='EMULATIONSTATION'
ORDER BY source.id,item.id LIMIT ?`, id, limit)
	if err != nil {
		return nil, fmt.Errorf("query interrupted EmulationStation reviews: %w", err)
	}
	defer func() { cleanup.Error("close interrupted EmulationStation reviews", rows.Close()) }()
	result := []application.ExecutionReview{}
	for rows.Next() {
		var value application.ExecutionReview
		var count int
		if err := rows.Scan(&value.ItemID, &value.State, &value.Version, &value.LibraryJobID, &value.LibraryItemID,
			&value.MetadataJSON, &value.WarningsJSON, &value.ReservedJobID, &value.ReservedItemID, &count); err != nil {
			return nil, fmt.Errorf("read interrupted EmulationStation review: %w", err)
		}
		if count != 1 || value.LibraryJobID != "" &&
			(value.LibraryJobID != value.ReservedJobID || value.LibraryItemID != value.ReservedItemID) {
			return nil, application.ErrInvalid
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
	item := change.Review
	if item.State == "PENDING" {
		if err := records.reviewState(ctx, item, "COPYING", change.NowMS); err != nil {
			return err
		}
		item.State, item.Version = "COPYING", item.Version+1
	}
	if item.State == "COPYING" {
		if err := records.reviewState(ctx, item, "VALIDATING", change.NowMS); err != nil {
			return err
		}
		item.State, item.Version = "VALIDATING", item.Version+1
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
		Set: `execution_state=?,version=version+1,updated_at_ms=?`, Values: []any{state, now},
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND execution_state=?`,
			Args:  []any{item.ItemID, item.Version, item.State},
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
