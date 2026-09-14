package emulationstationimport

import (
	"context"
	"encoding/json"
	"fmt"

	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/recordstore"
)

func (records leaseRecords) Claim(ctx context.Context, change application.ClaimLease) error {
	before, unit := change.Before, change.Execution
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET state='RUNNING',attempt_count=?,
execution_started_at_ms=?,execution_deadline_at_ms=?,leased_until_ms=?,heartbeat_at_ms=?,worker_id=?,
version=version+1,updated_at_ms=? WHERE id=? AND version=? AND state='QUEUED' AND kind=?
AND scope_type='EMULATIONSTATION_IMPORT' AND scope_id=? AND execution_no=? AND attempt_count=?
AND attempt_count<max_attempts AND available_at_ms<=? AND execution_started_at_ms IS ?
AND COALESCE(execution_deadline_at_ms,0)=? AND (execution_deadline_at_ms IS NULL OR execution_deadline_at_ms>?)
AND EXISTS(SELECT 1 FROM emulationstation_imports plan WHERE plan.id=? AND plan.version=? AND plan.state=?
AND ((jobs.kind='SERVER_EMULATIONSTATION_SCAN' AND plan.scan_job_id=jobs.id AND plan.import_job_id IS NULL)
OR (jobs.kind='SERVER_EMULATIONSTATION_IMPORT' AND plan.import_job_id=jobs.id)))`,
		unit.Attempt, change.StartedAtMS, unit.DeadlineAtMS, change.UntilMS, change.NowMS, unit.WorkerID, change.NowMS,
		unit.JobID, before.JobVersion, unit.Kind, unit.ImportID, unit.ExecutionNo, before.Attempt, change.NowMS,
		before.StartedAtMS, before.DeadlineAtMS, change.NowMS, unit.ImportID, before.ImportVersion, before.ImportState)
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	result, err = recordstore.UpdateEmulationstationImports(ctx, records.executor, recordstore.Update{
		Set: `state=?,phase=?,started_at_ms=CASE WHEN ?='SERVER_EMULATIONSTATION_IMPORT' THEN COALESCE(started_at_ms,?)
ELSE started_at_ms END,version=version+1,updated_at_ms=?`,
		Values: []any{change.ImportState, change.Phase, unit.Kind, change.NowMS, change.NowMS},
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state=?`,
			Args:  []any{unit.ImportID, before.ImportVersion, before.ImportState},
		},
	})
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	event, err := json.Marshal(struct {
		SchemaVersion int   `json:"schemaVersion"`
		ExecutionNo   int64 `json:"executionNo"`
		Attempt       int64 `json:"attempt"`
	}{1, unit.ExecutionNo, unit.Attempt})
	if err != nil {
		return fmt.Errorf("encode EmulationStation claim event: %w", err)
	}
	result, err = records.executor.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'EMULATIONSTATION_IMPORT',?,'STARTED',?,?)`,
		unit.JobID,
		unit.ImportID,
		string(event),
		change.NowMS,
	)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}
