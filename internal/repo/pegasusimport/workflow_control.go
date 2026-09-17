package pegasusimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	payloadmodel "retrom/internal/model/payloadrelease"
	payload "retrom/internal/repo/payloadrelease"

	application "retrom/internal/model/pegasusimport"
	"retrom/internal/repo/dbexec"
)

type PayloadTerminator interface {
	TerminalSources(context.Context, payloadmodel.ReleaseScope, payloadmodel.SourceBatch, int64) error
}

type WorkflowControl struct {
	database *sql.DB
	payloads PayloadTerminator
}

func NewWorkflowControl(database *sql.DB, payloads PayloadTerminator) *WorkflowControl {
	return &WorkflowControl{database: database, payloads: payloads}
}

func (repository *WorkflowControl) CommitCancelWorkflow(
	ctx context.Context, cmd application.CancelWorkflowCommand,
) (application.WorkflowSnapshot, bool, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return application.WorkflowSnapshot{}, false, fmt.Errorf("begin Pegasus cancel: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := workflowRecords{transaction: tx}
	before, version, err := readAndValidateCancellation(ctx, records, cmd)
	if err != nil {
		return application.WorkflowSnapshot{}, false, err
	}
	if !application.CanCancelWorkflow(before, version) {
		return application.WorkflowSnapshot{}, false, application.ErrNotCancellable
	}
	plan := buildCancellationPlan(before, cmd)
	if err := records.Cancel(ctx, plan); err != nil {
		return application.WorkflowSnapshot{}, false, fmt.Errorf("save Pegasus cancellation: %w", err)
	}
	if err := repository.scheduleTerminalPayloads(ctx, tx, plan); err != nil {
		return application.WorkflowSnapshot{}, false, err
	}
	after, err := records.Current(ctx, before.Summary.ID)
	if err != nil {
		return application.WorkflowSnapshot{}, false, fmt.Errorf("read cancelled Pegasus import: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return application.WorkflowSnapshot{}, false, fmt.Errorf("commit Pegasus cancel: %w", err)
	}
	return after, plan.Pending, nil
}

func readAndValidateCancellation(
	ctx context.Context, records workflowRecords, cmd application.CancelWorkflowCommand,
) (application.WorkflowSnapshot, int64, error) {
	var before application.WorkflowSnapshot
	var err error
	if cmd.ByJob {
		before, err = records.CurrentJob(ctx, cmd.ID)
	} else {
		before, err = records.Current(ctx, cmd.ID)
	}
	if err != nil {
		return before, 0, fmt.Errorf("read Pegasus cancellation: %w", err)
	}
	version := cmd.Version
	if cmd.ByJob {
		if !application.MatchesCancellationJob(before, cmd.ID, cmd.Kind, cmd.ScopeID) {
			return before, 0, application.ErrNotCancellable
		}
		if cmd.Version != before.JobVersion || cmd.Version < 1 {
			return before, 0, application.ErrVersionConflict
		}
		version = before.Summary.Version
	}
	return before, version, nil
}

func buildCancellationPlan(
	before application.WorkflowSnapshot, cmd application.CancelWorkflowCommand,
) application.CancellationPlan {
	pending := before.JobState == "RUNNING" || before.Summary.State == "RUNNING"
	state := "CANCELLED"
	if pending {
		state = "CANCEL_REQUESTED"
	}
	plan := application.CancellationPlan{
		Before: before, Reason: cmd.Reason, ActorID: cmd.ActorID, AuditID: cmd.AuditID,
		NowMS: cmd.NowMS, State: state, Pending: pending,
	}
	if !pending {
		plan.CompletedAtMS = &cmd.NowMS
	}
	return plan
}

func (repository *WorkflowControl) scheduleTerminalPayloads(
	ctx context.Context, tx *sql.Tx, plan application.CancellationPlan,
) error {
	if plan.Before.Summary.ImportJobID != nil && repository.payloads != nil {
		scope := payload.BindReleases(tx)
		batch := payloadmodel.SourceBatch{
			Type: payloadmodel.ScopePegasusImportItem, ImportID: plan.Before.Summary.ID,
		}
		if err := repository.payloads.TerminalSources(ctx, scope, batch, plan.NowMS); err != nil {
			return fmt.Errorf("schedule terminal Pegasus payloads: %w", err)
		}
	}
	return nil
}

func (repository *WorkflowControl) CommitRetryWorkflow(
	ctx context.Context, cmd application.RetryWorkflowCommand,
) (application.Summary, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return application.Summary{}, fmt.Errorf("begin Pegasus retry: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := workflowRecords{transaction: tx}
	before, err := records.Current(ctx, cmd.ID)
	if err != nil {
		return application.Summary{}, fmt.Errorf("read Pegasus retry: %w", err)
	}
	if !application.CanRetry(before, cmd.Version) {
		return application.Summary{}, application.ErrNotRetryable
	}
	plan := application.RetryPlan{
		Before:      before,
		Execution:   before.Execution + 1,
		ExecutionID: cmd.ExecutionID,
		AuditID:     cmd.AuditID,
		ActorID:     cmd.ActorID,
		NowMS:       cmd.NowMS,
	}
	if err := records.Retry(ctx, plan); err != nil {
		return application.Summary{}, fmt.Errorf("save Pegasus retry: %w", err)
	}
	after, err := records.Current(ctx, cmd.ID)
	if err != nil {
		return application.Summary{}, fmt.Errorf("read retried Pegasus import: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return application.Summary{}, fmt.Errorf("commit Pegasus retry: %w", err)
	}
	return after.Summary, nil
}

type workflowRecords struct{ transaction *sql.Tx }

func (records workflowRecords) Current(ctx context.Context, id string) (application.WorkflowSnapshot, error) {
	summary, err := (&Queries{database: records.transaction}).Get(ctx, id)
	if err != nil {
		return application.WorkflowSnapshot{}, err
	}
	result := application.WorkflowSnapshot{Summary: summary}
	jobID, kind := workflowJob(summary)
	err = records.transaction.QueryRowContext(ctx, `
SELECT state,version,execution_no FROM jobs WHERE id=? AND scope_type='PEGASUS_IMPORT'
AND scope_id=? AND kind=?`, jobID, id, kind).
		Scan(&result.JobState, &result.JobVersion, &result.Execution)
	if err != nil {
		return application.WorkflowSnapshot{}, fmt.Errorf("read Pegasus execution identity: %w", err)
	}
	if summary.ImportJobID == nil {
		return result, nil
	}
	err = records.transaction.QueryRowContext(ctx, `
SELECT EXISTS(SELECT 1 FROM pegasus_imports WHERE id<>?
AND import_job_id IS NOT NULL AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')),
(SELECT count(*) FROM pegasus_import_items WHERE import_id=? AND retryable=1
AND execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED'))`, id, id).
		Scan(&result.OtherActive, &result.RetryableItems)
	if err != nil {
		return application.WorkflowSnapshot{}, fmt.Errorf("read Pegasus retry availability: %w", err)
	}
	return result, nil
}

func workflowJob(summary application.Summary) (string, string) {
	if summary.ImportJobID != nil {
		return *summary.ImportJobID, "SERVER_PEGASUS_IMPORT"
	}
	return summary.ScanJobID, "SERVER_PEGASUS_SCAN"
}

func (records workflowRecords) CurrentJob(ctx context.Context, jobID string) (application.WorkflowSnapshot, error) {
	var importID string
	err := records.transaction.QueryRowContext(ctx, `SELECT plan.id FROM jobs job
JOIN pegasus_imports plan ON plan.id=job.scope_id WHERE job.id=? AND job.scope_type='PEGASUS_IMPORT'
AND ((job.kind='SERVER_PEGASUS_SCAN' AND plan.scan_job_id=job.id AND plan.import_job_id IS NULL)
OR(job.kind='SERVER_PEGASUS_IMPORT' AND plan.import_job_id=job.id))`, jobID).Scan(&importID)
	if errors.Is(err, sql.ErrNoRows) {
		return application.WorkflowSnapshot{}, application.ErrNotFound
	}
	if err != nil {
		return application.WorkflowSnapshot{}, fmt.Errorf("read Pegasus cancellation job ownership: %w", err)
	}
	return records.Current(ctx, importID)
}

func requireWorkflowChange(result sql.Result, err, conflict error) error {
	if err != nil {
		return fmt.Errorf("change Pegasus workflow: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read Pegasus workflow change count: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("pegasus workflow conflict: %w", conflict)
	}
	return nil
}

type workflowAudit struct {
	ID, ActorID, ImportID, Action string
	NowMS                         int64
}

func (records workflowRecords) audit(ctx context.Context, audit workflowAudit) error {
	_, err := records.transaction.ExecContext(ctx, `
INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'USER',?,NULL,?,'PEGASUS_IMPORT',?,'{}','{}',NULL,NULL,?)`,
		audit.ID, audit.ActorID, audit.Action, audit.ImportID, audit.NowMS)
	if err != nil {
		return fmt.Errorf("record Pegasus workflow audit: %w", err)
	}
	return nil
}
