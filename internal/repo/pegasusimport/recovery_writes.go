package pegasusimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/repo/recordstore"
	application "retrom/internal/service/pegasusimport"
)

func (records recoveryRecords) CompleteReview(ctx context.Context, change application.RecoveryReviewChange) error {
	execution := change.Execution
	current, err := records.Current(ctx, execution.JobID)
	if err != nil {
		return err
	}
	if current != execution || execution.LeaseUntilMS > change.Handoff.NowMS {
		return application.ErrVersionConflict
	}
	before := change.Handoff.Before
	identity := before.Identity
	warnings, err := json.Marshal(change.Handoff.Warnings)
	if err != nil {
		return fmt.Errorf("encode recovered Pegasus warnings: %w", err)
	}
	result, err := recordstore.UpdatePegasusImportItems(ctx, records.tx, recordstore.Update{
		Set: `execution_state='REVIEW_PENDING',error_code=NULL,error_details_json=NULL,retryable=0,
 warnings_json=?,completed_at_ms=?,
version=version+1,updated_at_ms=?`,
		Values: []any{string(warnings), change.Handoff.NowMS, change.Handoff.NowMS},
		Scope: recordstore.Scope{
			Where: `id=? AND import_id=? AND version=? AND execution_state=?
 AND library_import_job_id=? AND library_import_item_id=?
 AND EXISTS(SELECT 1 FROM import_items WHERE id=? AND import_job_id=? AND state='REVIEW_PENDING')
 AND EXISTS(SELECT 1 FROM pegasus_imports plan JOIN jobs job ON job.id=plan.import_job_id
 WHERE plan.id=? AND plan.version=? AND plan.state=? AND job.id=? AND job.version=?
 AND job.state=? AND job.execution_no=? AND job.attempt_count=? AND COALESCE(job.worker_id,'')=?
 AND COALESCE(job.leased_until_ms,0)=? AND ((job.state='QUEUED' AND job.leased_until_ms IS NULL)
 OR (job.state IN ('RUNNING','CANCEL_REQUESTED') AND job.leased_until_ms<=?)) AND job.execution_deadline_at_ms=?)`,
			Args: []any{
				identity.ItemID, identity.ImportID, before.Version, before.State, identity.LibraryJobID, identity.LibraryItemID,
				identity.LibraryItemID, identity.LibraryJobID, execution.ImportID, execution.ImportVersion, execution.ImportState,
				execution.JobID, execution.JobVersion, execution.JobState, execution.ExecutionNo, execution.Attempt,
				execution.WorkerID,
				execution.LeaseUntilMS, change.Handoff.NowMS, execution.DeadlineMS,
			},
		},
	})
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	return RefreshCountsAndEvent(
		ctx,
		records.tx,
		execution.JobID,
		execution.ImportID,
		identity.ItemID,
		"REVIEW_PENDING",
		change.Handoff.NowMS,
	)
}

func (records recoveryRecords) Apply(ctx context.Context, change application.RecoveryChange) error {
	before := change.Before
	var finished *int64
	if change.JobState != "QUEUED" {
		finished = &change.NowMS
	}
	result, err := records.tx.ExecContext(ctx, `UPDATE jobs SET state=?,available_at_ms=?,finished_at_ms=?,
 leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,error_code=?,error_retryable=0,
version=version+1,updated_at_ms=?
 WHERE id=? AND version=? AND state=? AND execution_no=? AND attempt_count=? AND COALESCE(worker_id,'')=?
 AND COALESCE(leased_until_ms,0)=? AND ((state='QUEUED' AND leased_until_ms IS NULL)
 OR (state IN ('RUNNING','CANCEL_REQUESTED') AND leased_until_ms<=?)) AND execution_deadline_at_ms=?
 AND EXISTS(SELECT 1 FROM pegasus_imports plan WHERE plan.id=? AND plan.version=? AND plan.state=?
 AND ((jobs.kind='SERVER_PEGASUS_IMPORT' AND plan.import_job_id=jobs.id)
 OR (jobs.kind='SERVER_PEGASUS_SCAN' AND plan.scan_job_id=jobs.id AND plan.import_job_id IS NULL)))`,
		change.JobState, change.NowMS, finished, optionalText(change.Code), change.NowMS, before.JobID, before.JobVersion,
		before.JobState, before.ExecutionNo, before.Attempt, before.WorkerID, before.LeaseUntilMS, change.NowMS,
		before.DeadlineMS,
		before.ImportID, before.ImportVersion, before.ImportState)
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	if before.Kind == "SERVER_PEGASUS_SCAN" {
		if err := ClearUnpublishedScan(ctx, records.tx, before.ImportID); err != nil {
			return err
		}
	} else if err := records.recoverItems(ctx, change, finished); err != nil {
		return err
	}
	// Refresh all mutually constrained counts in one write before closing the parent.
	if err := refreshCounts(ctx, records.tx, before.ImportID, change.NowMS); err != nil {
		return err
	}
	result, err = recordstore.UpdatePegasusImports(ctx, records.tx, recordstore.Update{
		Set: `state=?,phase=NULL,last_error_code=?,retryable=0,completed_at_ms=?,
version=version+1,updated_at_ms=?`,
		Values: []any{change.ImportState, optionalText(change.Code), finished, change.NowMS},
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state=?`,
			Args:  []any{before.ImportID, before.ImportVersion + 1, before.ImportState},
		},
	})
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	return records.recoveryEvent(ctx, change)
}

func (records recoveryRecords) recoverItems(
	ctx context.Context,
	change application.RecoveryChange,
	finished *int64,
) error {
	scope := `import_id=? AND execution_state IN ('PENDING','COPYING','VALIDATING')`
	if change.JobState == "QUEUED" {
		scope = `import_id=? AND execution_state IN ('COPYING','VALIDATING')`
	}
	_, err := recordstore.UpdatePegasusImportItems(ctx, records.tx, recordstore.Update{
		Set: `execution_state=?,error_code=?,error_details_json=NULL,retryable=0,completed_at_ms=?,
version=version+1,updated_at_ms=?`,
		Values: []any{change.ItemState, optionalText(change.ItemCode), finished, change.NowMS},
		Scope:  recordstore.Scope{Where: scope, Args: []any{change.Before.ImportID}},
	})
	if err != nil {
		return fmt.Errorf("recover unfinished Pegasus items: %w", err)
	}
	return nil
}

func (records recoveryRecords) recoveryEvent(ctx context.Context, change application.RecoveryChange) error {
	data, err := json.Marshal(struct {
		SchemaVersion int    `json:"schemaVersion"`
		ExecutionNo   int64  `json:"executionNo"`
		Code          string `json:"code,omitempty"`
	}{1, change.Before.ExecutionNo, change.Code})
	if err != nil {
		return fmt.Errorf("encode Pegasus recovery event: %w", err)
	}
	_, err = records.tx.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
 VALUES(?,'PEGASUS_IMPORT',?,?,?,?)`,
		change.Before.JobID,
		change.Before.ImportID,
		change.Event,
		string(data),
		change.NowMS,
	)
	if err != nil {
		return fmt.Errorf("record Pegasus recovery event: %w", err)
	}
	return nil
}

func optionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
