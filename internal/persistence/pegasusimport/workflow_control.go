package pegasusimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	payload "retrom/internal/persistence/payloadrelease"

	"retrom/internal/dbexec"
	application "retrom/internal/service/pegasusimport"
)

type WorkflowControl struct{ database *sql.DB }

func NewWorkflowControl(database *sql.DB) *WorkflowControl {
	return &WorkflowControl{database: database}
}

func (repository *WorkflowControl) WithControl(ctx context.Context, work func(application.WorkflowScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Pegasus workflow control: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := workflowRecords{transaction: tx}
	if err := work(application.WorkflowScope{
		Payload: payload.BindReleases(tx), Read: records, Write: records,
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Pegasus workflow control: %w", err)
	}
	return nil
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
