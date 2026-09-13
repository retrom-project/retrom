package serverimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
	"retrom/internal/service/serverimport"
)

type Creation struct{ database *sql.DB }

func NewCreation(database *sql.DB) *Creation { return &Creation{database: database} }
func (repository *Creation) WithCreate(ctx context.Context, work func(serverimport.CreationWriter) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin import creation: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := work(creationRecords{tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit import creation: %w", err)
	}
	return nil
}

type creationRecords struct{ executor dbexec.Executor }

func (records creationRecords) Active(ctx context.Context, kind string) (bool, error) {
	var active bool
	err := records.executor.QueryRowContext(ctx, `
SELECT EXISTS(SELECT 1 FROM server_imports WHERE kind=? AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED'))
`, kind).Scan(&active)
	if err != nil {
		return false, fmt.Errorf("read active server import: %w", err)
	}
	return active, nil
}

func (records creationRecords) Insert(
	ctx context.Context,
	plan serverimport.CreationPlan,
) (serverimport.Summary, error) {
	if err := records.insertJob(ctx, plan); err != nil {
		return serverimport.Summary{}, err
	}
	if err := records.insertImport(ctx, plan); err != nil {
		return serverimport.Summary{}, err
	}
	for _, item := range plan.Items {
		if err := insertCatalogItem(ctx, records.executor, plan.ImportID, item, plan.Evidence.Now); err != nil {
			return serverimport.Summary{}, err
		}
	}
	if err := records.creationEvidence(ctx, plan); err != nil {
		return serverimport.Summary{}, err
	}
	return getSummary(ctx, records.executor, plan.ImportID)
}

func (records creationRecords) insertJob(ctx context.Context, plan serverimport.CreationPlan) error {
	if _, err := records.executor.ExecContext(
		ctx,
		`
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'SERVER_IMPORT',?,'SERVER_BIOS_IMPORT',?,1,?,1,'QUEUED',0,4,1,?,?,?)
`,
		plan.JobID,
		plan.ImportID,
		plan.DedupeKey,
		string(
			plan.Payload,
		),
		plan.Evidence.Now,
		plan.Evidence.Now,
		plan.Evidence.Now,
	); err != nil {
		return fmt.Errorf("create import job: %w", err)
	}
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES(?,1,?,?,?)
`, plan.JobID, string(plan.Input), plan.InputDigest, plan.Evidence.Now); err != nil {
		return fmt.Errorf("create import input: %w", err)
	}
	return nil
}

func (records creationRecords) insertImport(ctx context.Context, plan serverimport.CreationPlan) error {
	result, err := recordstore.CreateServerImports(ctx, records.executor, `
INSERT INTO server_imports(id,kind,root_id,root_label_snapshot,source_relative_path,root_config_digest,
catalog_snapshot_digest,replace_if_better,state,catalog_item_count,job_id,created_by_user_id,
version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,'QUEUED',?,?,?,1,?,?)
ON CONFLICT(kind) WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED') DO NOTHING
`, plan.ImportID, plan.Request.Kind, plan.Root.ID, plan.Root.Label, plan.Request.SourceRelativePath,
		plan.Root.Digest, plan.CatalogDigest, plan.Request.ReplaceIfBetter, len(plan.Items), plan.JobID,
		plan.Evidence.ActorID, plan.Evidence.Now, plan.Evidence.Now)
	return requireControlChange(result, err, serverimport.ErrActive)
}

func (records creationRecords) creationEvidence(ctx context.Context, plan serverimport.CreationPlan) error {
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'QUEUED',?,?)
`, plan.JobID, plan.ImportID, string(plan.Evidence.Event), plan.Evidence.Now); err != nil {
		return fmt.Errorf("create import event: %w", err)
	}
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'USER',?,NULL,'SERVER_IMPORT_CREATED','SERVER_IMPORT',?,NULL,?,NULL,NULL,?)
`, plan.Evidence.AuditID, plan.Evidence.ActorID, plan.ImportID, string(plan.Audit), plan.Evidence.Now); err != nil {
		return fmt.Errorf("create import audit: %w", err)
	}
	return nil
}
