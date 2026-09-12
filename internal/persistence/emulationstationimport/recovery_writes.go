package emulationstationimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/emulationstationimport"
)

func (records recoveryRecords) Apply(ctx context.Context, change application.RecoveryChange) error {
	if err := records.fence(ctx, change); err != nil {
		return err
	}
	if change.Before.Kind == "SERVER_EMULATIONSTATION_SCAN" {
		if err := clearUnpublishedScan(ctx, records.executor, change.Before.ImportID); err != nil {
			return err
		}
	} else if change.JobState != "QUEUED" {
		if err := records.terminalItems(ctx, change); err != nil {
			return err
		}
	}
	if err := records.job(ctx, change); err != nil {
		return err
	}
	if err := records.aggregate(ctx, change); err != nil {
		return err
	}
	if change.JobState != "QUEUED" && change.Before.Kind == "SERVER_EMULATIONSTATION_IMPORT" {
		if err := ScheduleTerminalItems(ctx, records.transaction, change.Before.ImportID, change.NowMS); err != nil {
			return err
		}
	}
	return records.event(ctx, change)
}

func (records recoveryRecords) terminalItems(ctx context.Context, change application.RecoveryChange) error {
	code := change.Code
	if change.ItemState == "CANCELLED" {
		code = "CANCELLED"
	}
	var count int64
	if err := records.executor.QueryRowContext(ctx, `SELECT count(*) FROM emulationstation_import_items
WHERE import_id=? AND execution_state IN ('PENDING','COPYING','VALIDATING')`, change.Before.ImportID).Scan(
		&count,
	); err != nil {
		return fmt.Errorf("count interrupted EmulationStation items: %w", err)
	}
	result, err := recordstore.UpdateEmulationstationImportItems(ctx, records.executor, recordstore.Update{
		Set: `execution_state=?,error_code=?,error_details_json=NULL,retryable=0,
completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Values: []any{change.ItemState, code, change.NowMS, change.NowMS},
		Scope: recordstore.Scope{
			Where: `import_id=? AND execution_state IN ('PENDING','COPYING','VALIDATING')`,
			Args:  []any{change.Before.ImportID},
		},
	})
	return requireRecoveryCount(result, err, count)
}

func (records recoveryRecords) job(ctx context.Context, change application.RecoveryChange) error {
	var finished *int64
	var retryable any
	available := change.Before.AvailableAtMS
	if change.JobState == "QUEUED" {
		available = change.AvailableAtMS
	} else {
		finished = &change.NowMS
	}
	if change.JobState == "FAILED" {
		retryable = 0
	}
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET state=?,available_at_ms=?,finished_at_ms=?,
leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,error_code=?,error_retryable=?,
version=version+1,updated_at_ms=?
WHERE id=? AND version=? AND state=?`, change.JobState, available, finished, optionalText(change.Code), retryable,
		change.NowMS, change.Before.JobID, change.Before.JobVersion, change.Before.JobState)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}

func (records recoveryRecords) aggregate(ctx context.Context, change application.RecoveryChange) error {
	var finished *int64
	if change.JobState != "QUEUED" {
		finished = &change.NowMS
	}
	counts, err := LoadTerminalItemCounts(ctx, records.executor, change.Before.ImportID)
	if err != nil {
		return err
	}
	set := `state=?,phase=?,last_error_code=?,retryable=0,completed_at_ms=?,
skipped_mapping_item_count=?,review_pending_item_count=?,published_item_count=?,review_discarded_item_count=?,
existing_item_count=?,blocked_item_count=?,failed_item_count=?,cancelled_item_count=?,version=version+1,updated_at_ms=?`
	if change.Before.Kind == "SERVER_EMULATIONSTATION_SCAN" {
		set += `,source_snapshot_digest=NULL,scan_completed_at_ms=NULL,gamelist_count=0,invalid_gamelist_count=0,
collection_count=0,folder_entry_count=0,game_count=0,estimated_source_bytes=0,mapped_collection_count=0,
skipped_collection_count=0,processable_item_count=0,media_warning_count=0,
discovered_cover_count=0,discovered_video_count=0`
	}
	values := make([]any, 0, 13)
	values = append(values, change.ImportState, optionalText(change.Phase), optionalText(change.Code), finished)
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

func (records recoveryRecords) event(ctx context.Context, change application.RecoveryChange) error {
	data := map[string]any{"schemaVersion": 1}
	switch change.Event {
	case "RETRY_SCHEDULED":
		data["executionNo"] = change.Before.ExecutionNo
		data["attempt"] = change.Before.Attempt
		data["retryAtMs"] = change.AvailableAtMS
		data["errorCode"] = "EMULATIONSTATION_WORKER_LEASE_EXPIRED"
		data["errorRetryable"] = true
	case "CANCELLED":
		data["recovered"] = true
	case "FAILED":
		data["code"] = change.Code
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode EmulationStation recovery event: %w", err)
	}
	result, err := records.executor.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'EMULATIONSTATION_IMPORT',?,?,?,?)`,
		change.Before.JobID,
		change.Before.ImportID,
		change.Event,
		string(encoded),
		change.NowMS,
	)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}

func optionalText(value string) any {
	if value == "" {
		return nil
	}
	return value
}
