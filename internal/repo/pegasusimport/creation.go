package pegasusimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"

	application "retrom/internal/model/pegasusimport"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
)

type Creation struct {
	database      *sql.DB
	preCommitHook func() error
}

func NewCreation(database *sql.DB) *Creation { return &Creation{database: database} }

func (repository *Creation) WithPreCommitHook(hook func() error) {
	repository.preCommitHook = hook
}

func (repository *Creation) LoadPendingPlanCount(ctx context.Context) (int, error) {
	var count int
	if err := repository.database.QueryRowContext(ctx, `
SELECT count(*) FROM pegasus_imports WHERE state IN ('SCANNING','AWAITING_MAPPING')
OR (state='CANCEL_REQUESTED' AND import_job_id IS NULL)
`).Scan(&count); err != nil {
		return 0, fmt.Errorf("read pending Pegasus plans: %w", err)
	}
	return count, nil
}

func (repository *Creation) CommitCreation(
	ctx context.Context, plan application.CreationPlan,
) (application.Summary, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return application.Summary{}, fmt.Errorf("begin Pegasus creation: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := creationRecords{executor: tx}
	summary, err := records.Insert(ctx, plan)
	if err != nil {
		return application.Summary{}, err
	}
	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(); err != nil {
			return application.Summary{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return application.Summary{}, fmt.Errorf("commit Pegasus creation: %w", err)
	}
	return summary, nil
}

type creationRecords struct{ executor dbexec.Executor }

func (records creationRecords) Insert(ctx context.Context, plan application.CreationPlan) (application.Summary, error) {
	if err := records.insertScanJob(ctx, plan); err != nil {
		return application.Summary{}, err
	}
	result, err := recordstore.CreatePegasusImports(
		ctx,
		records.executor,
		`
INSERT INTO pegasus_imports(id,root_id,root_label_snapshot,source_relative_path,root_config_digest,state,phase,
scan_job_id,created_by_user_id,created_at_ms,updated_at_ms,expires_at_ms)
SELECT ?,?,?,?,?,'SCANNING','DISCOVERING_METADATA',?,?,?,?,?
WHERE (SELECT count(*) FROM pegasus_imports WHERE state IN ('SCANNING','AWAITING_MAPPING')
OR (state='CANCEL_REQUESTED' AND import_job_id IS NULL))<20
`,
		plan.ImportID,
		plan.Root.ID,
		plan.Root.Label,
		plan.Request.SourceRelativePath,
		plan.Root.Digest,
		plan.JobID,
		plan.ActorID,
		plan.NowMS,
		plan.NowMS,
		plan.ExpiresAtMS,
	)
	if err != nil {
		return application.Summary{}, fmt.Errorf("insert Pegasus plan: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return application.Summary{}, fmt.Errorf(
			"read Pegasus creation count: %w",
			err,
		)
	}
	if changed != 1 {
		return application.Summary{}, application.ErrActive
	}
	if err := records.insertCreationEvidence(ctx, plan); err != nil {
		return application.Summary{}, err
	}
	return scanSummary(records.executor.QueryRowContext(ctx, summaryQuery+` WHERE import.id=?`, plan.ImportID))
}

func (records creationRecords) insertScanJob(ctx context.Context, plan application.CreationPlan) error {
	input := map[string]any{
		"schemaVersion": 1, "kind": "SERVER_PEGASUS_SCAN",
		"scope": map[string]any{"type": "PEGASUS_IMPORT", "id": plan.ImportID}, "executionId": plan.ExecutionID,
		"inputs": map[string]any{
			"rootId":             plan.Root.ID,
			"sourceRelativePath": plan.Request.SourceRelativePath,
			"rootConfigDigest":   plan.Root.Digest,
		},
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode Pegasus creation input: %w", err)
	}
	digest := sha256.Sum256(encoded)
	if _, err := records.executor.ExecContext(
		ctx,
		`
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'SERVER_PEGASUS_SCAN',?,1,'{"inputExecutionNo":1}',1,'QUEUED',0,4,1,?,?,?)
`,
		plan.JobID,
		plan.ImportID,
		plan.DedupeKey,
		plan.NowMS,
		plan.NowMS,
		plan.NowMS,
	); err != nil {
		return fmt.Errorf(
			"insert Pegasus scan job: %w",
			err,
		)
	}
	if _, err := records.executor.ExecContext(
		ctx,
		`INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES(?,1,?,?,?)`,
		plan.JobID,
		string(encoded),
		hex.EncodeToString(digest[:]),
		plan.NowMS,
	); err != nil {
		return fmt.Errorf(
			"insert Pegasus scan input: %w",
			err,
		)
	}
	return nil
}

func (records creationRecords) insertCreationEvidence(ctx context.Context, plan application.CreationPlan) error {
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'QUEUED','{"schemaVersion":1,"executionNo":1,"attempt":0}',?)
`, plan.JobID, plan.ImportID, plan.NowMS); err != nil {
		return fmt.Errorf("insert Pegasus creation event: %w", err)
	}
	if _, err := records.executor.ExecContext(
		ctx,
		`
INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'USER',?,NULL,'PEGASUS_IMPORT_CREATED','PEGASUS_IMPORT',?,NULL,'{"state":"SCANNING"}',NULL,NULL,?)
`,
		plan.AuditID,
		plan.ActorID,
		plan.ImportID,
		plan.NowMS,
	); err != nil {
		return fmt.Errorf(
			"insert Pegasus creation audit: %w",
			err,
		)
	}
	return nil
}
