package emulationstationimport

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/emulationstationimport"
)

func (records executionRecords) Finish(ctx context.Context, change application.ExecutionFinish) error {
	if err := records.fence(ctx, change); err != nil {
		return err
	}
	scan := change.Before.Kind == "SERVER_EMULATIONSTATION_SCAN"
	clearScan := scan && change.JobState != "FAILED"
	if clearScan {
		if err := clearUnpublishedScan(ctx, records.executor, change.Before.ImportID); err != nil {
			return err
		}
	} else if !scan && change.JobState != "QUEUED" {
		if err := records.terminalItems(ctx, change); err != nil {
			return err
		}
	}
	if err := records.finishJob(ctx, change); err != nil {
		return err
	}
	if err := records.finishAggregate(ctx, change, clearScan); err != nil {
		return err
	}
	if !scan && change.JobState != "QUEUED" {
		if err := ScheduleTerminalItems(ctx, records.transaction, change.Before.ImportID, change.NowMS); err != nil {
			return err
		}
	}
	return records.finishEvent(ctx, change)
}

func (records executionRecords) terminalItems(ctx context.Context, change application.ExecutionFinish) error {
	code := change.Code
	if change.JobState == "CANCELLED" {
		code = "CANCELLED"
	}
	var count int64
	if err := records.executor.QueryRowContext(ctx, `SELECT count(*) FROM emulationstation_import_items
WHERE import_id=? AND execution_state IN ('PENDING','COPYING','VALIDATING')`, change.Before.ImportID).Scan(
		&count,
	); err != nil {
		return fmt.Errorf("count unfinished EmulationStation items: %w", err)
	}
	result, err := recordstore.UpdateEmulationstationImportItems(ctx, records.executor, recordstore.Update{
		Set: `execution_state=?,error_code=?,error_details_json=NULL,retryable=?,completed_at_ms=?,
version=version+1,updated_at_ms=?`,
		Values: []any{change.ItemState, code, change.Retryable, change.NowMS, change.NowMS},
		Scope: recordstore.Scope{
			Where: `import_id=? AND execution_state IN ('PENDING','COPYING','VALIDATING')`,
			Args:  []any{change.Before.ImportID},
		},
	})
	return requireRecoveryCount(result, err, count)
}

func (records executionRecords) finishJob(ctx context.Context, change application.ExecutionFinish) error {
	var finished *int64
	var code, retryable any
	available := change.Before.AvailableAtMS
	if change.JobState == "QUEUED" {
		available = change.AvailableAtMS
	} else {
		finished = &change.NowMS
	}
	if change.JobState == "FAILED" {
		code, retryable = change.Code, change.Retryable
	}
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET state=?,available_at_ms=?,finished_at_ms=?,
leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,error_code=?,error_retryable=?,
version=version+1,updated_at_ms=?
WHERE id=? AND version=? AND state=?`, change.JobState, available, finished, code, retryable, change.NowMS,
		change.Before.JobID, change.Before.JobVersion, change.Before.JobState)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}

func (records executionRecords) finishAggregate(
	ctx context.Context, change application.ExecutionFinish, clearScan bool,
) error {
	counts, err := LoadTerminalItemCounts(ctx, records.executor, change.Before.ImportID)
	if err != nil {
		return err
	}
	var finished *int64
	var code any
	if change.JobState != "QUEUED" {
		finished = &change.NowMS
	}
	if change.JobState == "FAILED" {
		code = change.Code
	}
	set := `state=?,phase=?,last_error_code=?,completed_at_ms=?,retryable=EXISTS(
SELECT 1 FROM emulationstation_import_items WHERE import_id=? AND retryable=1
AND execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')),
skipped_mapping_item_count=?,review_pending_item_count=?,published_item_count=?,review_discarded_item_count=?,
existing_item_count=?,blocked_item_count=?,failed_item_count=?,cancelled_item_count=?,version=version+1,updated_at_ms=?`
	if clearScan {
		set += `,source_snapshot_digest=NULL,scan_completed_at_ms=NULL,gamelist_count=0,invalid_gamelist_count=0,
collection_count=0,folder_entry_count=0,game_count=0,estimated_source_bytes=0,mapped_collection_count=0,
skipped_collection_count=0,processable_item_count=0,media_warning_count=0,
discovered_cover_count=0,discovered_video_count=0`
	}
	values := make([]any, 0, 14)
	values = append(values, change.ImportState, optionalText(change.Phase), code, finished, change.Before.ImportID)
	values = append(values, terminalCountValues(counts)...)
	values = append(values, change.NowMS)
	result, err := recordstore.UpdateEmulationstationImports(ctx, records.executor, recordstore.Update{
		Set: set, Values: values, Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state=?`,
			Args:  []any{change.Before.ImportID, change.Before.ImportVersion, change.Before.ImportState},
		},
	})
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}
