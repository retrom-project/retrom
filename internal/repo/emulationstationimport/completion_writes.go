package emulationstationimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/repo/recordstore"
	application "retrom/internal/service/emulationstationimport"
)

func (records completionRecords) Complete(ctx context.Context, change application.CompletionChange) error {
	if err := executionRecords(records).Fence(ctx, change.Before, change.NowMS); err != nil {
		return err
	}
	values := make([]any, 0, 13)
	values = append(values, change.ImportState)
	values = append(values, terminalCountValues(change.Counts.Terminal)...)
	values = append(values, change.Counts.MediaWarnings, change.Retryable, change.NowMS, change.NowMS)
	result, err := recordstore.UpdateEmulationstationImports(ctx, records.executor, recordstore.Update{
		Set: `state=?,phase=NULL,skipped_mapping_item_count=?,review_pending_item_count=?,published_item_count=?,
review_discarded_item_count=?,existing_item_count=?,blocked_item_count=?,failed_item_count=?,cancelled_item_count=?,
media_warning_count=?,retryable=?,completed_at_ms=?,version=version+1,updated_at_ms=?`, Values: values,
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state='RUNNING'`,
			Args:  []any{change.Before.ImportID, change.Before.ImportVersion},
		},
	})
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	result, err = records.executor.ExecContext(
		ctx,
		`UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,
heartbeat_at_ms=NULL,worker_id=NULL,error_code=NULL,error_retryable=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND version=? AND state='RUNNING'`,
		change.NowMS,
		change.NowMS,
		change.Before.JobID,
		change.Before.JobVersion,
	)
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	return records.event(ctx, change)
}

func (records completionRecords) event(ctx context.Context, change application.CompletionChange) error {
	counts := change.Counts.Terminal
	encoded, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "state": change.ImportState, "reviewPending": counts.ReviewPending, "published": counts.Published,
		"reviewDiscarded": counts.ReviewDiscarded,
		"existing":        counts.Existing, "blocked": counts.Blocked, "failed": counts.Failed,
	})
	if err != nil {
		return fmt.Errorf("encode EmulationStation completion event: %w", err)
	}
	result, err := records.executor.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'EMULATIONSTATION_IMPORT',?,'SUCCEEDED',?,?)`,
		change.Before.JobID,
		change.Before.ImportID,
		string(encoded),
		change.NowMS,
	)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}
