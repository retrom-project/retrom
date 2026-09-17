package emulationstationimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	payloadmodel "retrom/internal/model/payloadrelease"
	payload "retrom/internal/repo/payloadrelease"

	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
)

type PayloadTerminator interface {
	TerminalSources(context.Context, payloadmodel.ReleaseScope, payloadmodel.SourceBatch, int64) error
}

type WorkflowControl struct {
	database   *sql.DB
	payloads   PayloadTerminator
}

func NewWorkflowControl(database *sql.DB, payloads PayloadTerminator) *WorkflowControl {
	return &WorkflowControl{database: database, payloads: payloads}
}

func (repository *WorkflowControl) InspectRetry(ctx context.Context, id string) (application.RetrySnapshot, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.RetrySnapshot{}, fmt.Errorf("begin EmulationStation retry inspection: %w", err)
	}
	defer dbexec.Rollback(tx)
	result, err := (workflowRecords{transaction: tx, executor: tx}).RetryCurrent(ctx, id)
	if err != nil {
		return application.RetrySnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.RetrySnapshot{}, fmt.Errorf("finish EmulationStation retry inspection: %w", err)
	}
	return result, nil
}

func (repository *WorkflowControl) CommitCancelWorkflow(
	ctx context.Context, cmd application.CancelWorkflowCommand,
) (application.WorkflowSnapshot, bool, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return application.WorkflowSnapshot{}, false, fmt.Errorf("begin EmulationStation cancel: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := workflowRecords{transaction: tx, executor: tx}
	before, err := records.Current(ctx, cmd.ID)
	if errors.Is(err, application.ErrNotFound) {
		return application.WorkflowSnapshot{}, false, application.ErrNotCancellable
	}
	if err != nil {
		return application.WorkflowSnapshot{}, false, fmt.Errorf("read EmulationStation cancellation: %w", err)
	}
	version := cmd.Version
	if cmd.Job != nil {
		if !application.MatchesCancellationJob(before, cmd.Job.JobID, cmd.Job.Kind, cmd.Job.ScopeID) {
			return application.WorkflowSnapshot{}, false, application.ErrNotCancellable
		}
		if cmd.Job.ExpectedVersion < 1 || cmd.Job.ExpectedVersion != before.JobVersion {
			return application.WorkflowSnapshot{}, false, application.ErrVersionConflict
		}
		version = before.Summary.Version
	}
	if !application.CanCancelWorkflow(before, version) {
		return application.WorkflowSnapshot{}, false, application.ErrNotCancellable
	}
	pending := before.JobState == "RUNNING"
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
	if err := records.Cancel(ctx, plan); err != nil {
		return application.WorkflowSnapshot{}, false, fmt.Errorf("persist EmulationStation cancellation: %w", err)
	}
	if plan.Before.Summary.ImportJobID != nil && repository.payloads != nil {
		scope := payload.BindReleases(tx)
		batch := payloadmodel.SourceBatch{
			Type: payloadmodel.ScopeEmulationStationImportItem, ImportID: plan.Before.Summary.ID,
		}
		if err := repository.payloads.TerminalSources(ctx, scope, batch, plan.NowMS); err != nil {
			return application.WorkflowSnapshot{}, false, fmt.Errorf(
				"schedule terminal EmulationStation payloads: %w", err,
			)
		}
	}
	after, err := records.Current(ctx, before.Summary.ID)
	if err != nil {
		return application.WorkflowSnapshot{}, false, fmt.Errorf("read cancelled EmulationStation plan: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return application.WorkflowSnapshot{}, false, fmt.Errorf("commit EmulationStation cancel: %w", err)
	}
	return after, pending, nil
}

func (repository *WorkflowControl) CommitRetryWorkflow(
	ctx context.Context, cmd application.RetryWorkflowCommand,
) (application.Summary, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return application.Summary{}, fmt.Errorf("begin EmulationStation retry: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := workflowRecords{transaction: tx, executor: tx}
	current, err := records.RetryCurrent(ctx, cmd.Plan.Before.Summary.ID)
	if errors.Is(err, application.ErrNotFound) {
		return application.Summary{}, application.ErrNotRetryable
	}
	if err != nil {
		return application.Summary{}, fmt.Errorf("reread EmulationStation retry: %w", err)
	}
	if err := application.ValidateRetryEligibility(current, cmd.Version); err != nil {
		return application.Summary{}, err
	}
	if !application.SameRetryExecution(cmd.Plan.Before, current) {
		return application.Summary{}, application.ErrNotRetryable
	}
	if !application.SameFrozenSource(
		cmd.Plan.Before.Summary,
		current.Summary,
		cmd.Plan.Before.FrozenSourceSnapshot,
		current.FrozenSourceSnapshot,
	) {
		return application.Summary{}, application.ErrSourceChanged
	}
	plan := cmd.Plan
	plan.Before = current
	plan.NowMS = cmd.Plan.NowMS
	if err := records.Retry(ctx, plan); err != nil {
		return application.Summary{}, fmt.Errorf("persist EmulationStation retry: %w", err)
	}
	after, err := records.Current(ctx, current.Summary.ID)
	if err != nil {
		return application.Summary{}, fmt.Errorf("read retried EmulationStation plan: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return application.Summary{}, fmt.Errorf("commit EmulationStation retry: %w", err)
	}
	return after.Summary, nil
}

type workflowRecords struct {
	transaction *sql.Tx
	executor    dbexec.Executor
}

func (records workflowRecords) Current(ctx context.Context, id string) (application.WorkflowSnapshot, error) {
	summary, err := (&Queries{database: records.executor}).Get(ctx, id)
	if err != nil {
		return application.WorkflowSnapshot{}, err
	}
	result := application.WorkflowSnapshot{Summary: summary}
	jobID, kind := workflowJob(summary)
	err = records.executor.QueryRowContext(
		ctx,
		`SELECT state,version,execution_no FROM jobs
WHERE id=? AND scope_type='EMULATIONSTATION_IMPORT' AND scope_id=? AND kind=?`,
		jobID, id, kind,
	).
		Scan(&result.JobState, &result.JobVersion, &result.Execution)
	if err != nil {
		return application.WorkflowSnapshot{}, fmt.Errorf("read EmulationStation workflow execution: %w", err)
	}
	return result, nil
}

func (records workflowRecords) RetryCurrent(ctx context.Context, id string) (application.RetrySnapshot, error) {
	current, err := records.Current(ctx, id)
	if err != nil {
		return application.RetrySnapshot{}, err
	}
	result := application.RetrySnapshot{WorkflowSnapshot: current}
	if current.Summary.ImportJobID == nil ||
		current.Summary.State != "FAILED" && current.Summary.State != "PARTIAL_FAILURE" {
		return result, nil
	}
	err = records.executor.QueryRowContext(ctx, `SELECT
EXISTS(SELECT 1 FROM emulationstation_imports WHERE id<>? AND import_job_id IS NOT NULL
AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')),
(SELECT count(*) FROM emulationstation_import_items WHERE import_id=? AND retryable=1
AND execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED'))`, id, id).Scan(
		&result.OtherActive,
		&result.RetryableItems,
	)
	if err != nil {
		return application.RetrySnapshot{}, fmt.Errorf("read EmulationStation retry eligibility: %w", err)
	}
	source := frozenSourceRecords{executor: records.executor}
	result.FrozenSourceSnapshot, err = source.Read(ctx, id)
	if err != nil {
		return application.RetrySnapshot{}, err
	}
	result.TargetsValid, err = source.targetsValid(ctx, id)
	if err != nil {
		return application.RetrySnapshot{}, err
	}
	return result, nil
}

func requireWorkflowChange(result sql.Result, err, conflict error) error {
	if err != nil {
		return fmt.Errorf("change EmulationStation workflow: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read EmulationStation workflow change count: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("EmulationStation workflow conflict: %w", conflict)
	}
	return nil
}

type workflowAudit struct {
	ID, ActorID, ImportID, Action string
	NowMS                         int64
}

func (records workflowRecords) audit(ctx context.Context, value workflowAudit) error {
	result, err := records.executor.ExecContext(
		ctx,
		`INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,
resource_type,resource_id,before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'USER',?,NULL,?,'EMULATIONSTATION_IMPORT',?,'{}','{}',NULL,NULL,?)`,
		value.ID,
		value.ActorID,
		value.Action,
		value.ImportID,
		value.NowMS,
	)
	if err := requireWorkflowChange(result, err, application.ErrNotCancellable); err != nil {
		return fmt.Errorf("record EmulationStation workflow audit: %w", err)
	}
	return nil
}

func workflowJob(summary application.Summary) (string, string) {
	if summary.ImportJobID != nil {
		return *summary.ImportJobID, "SERVER_EMULATIONSTATION_IMPORT"
	}
	return summary.ScanJobID, "SERVER_EMULATIONSTATION_SCAN"
}
