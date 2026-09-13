package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"

	payload "retrom/internal/persistence/payloadrelease"

	"retrom/internal/persistence/dbexec"
	application "retrom/internal/service/emulationstationimport"
)

type WorkflowControl struct{ database *sql.DB }

func NewWorkflowControl(database *sql.DB) *WorkflowControl {
	return &WorkflowControl{database: database}
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

func (repository *WorkflowControl) WithControl(ctx context.Context, work func(application.WorkflowScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin EmulationStation workflow: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := workflowRecords{transaction: tx, executor: tx}
	if err := work(application.WorkflowScope{
		Payload: payload.BindReleases(tx), Read: records, Write: records,
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit EmulationStation workflow: %w", err)
	}
	return nil
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
