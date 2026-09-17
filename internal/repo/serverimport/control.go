package serverimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/model/serverimport"
	"retrom/internal/repo/dbexec"
)

type Control struct{ database *sql.DB }

func NewControl(database *sql.DB) *Control { return &Control{database} }
func (repository *Control) CommitWrite(ctx context.Context, work func(serverimport.ControlScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin server import control: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := controlRecords{tx}
	if err := work(serverimport.ControlScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit server import control: %w", err)
	}
	return nil
}

type controlRecords struct{ executor dbexec.Executor }

func (records controlRecords) Current(ctx context.Context, id string) (serverimport.ControlSnapshot, error) {
	summary, err := getSummary(ctx, records.executor, id)
	if err != nil {
		return serverimport.ControlSnapshot{}, err
	}
	snapshot := serverimport.ControlSnapshot{Summary: summary}
	err = records.executor.QueryRowContext(
		ctx,
		`
SELECT import.root_config_digest, import.catalog_snapshot_digest, job.state, job.version,
job.execution_no, (SELECT count(*) FROM server_bios_import_items WHERE server_import_id=import.id AND
state IN ('PENDING', 'EVALUATING')),
EXISTS(SELECT 1 FROM server_imports other WHERE other.kind=import.kind AND other.id<>import.id
AND other.state IN ('QUEUED','RUNNING','CANCEL_REQUESTED'))
FROM server_imports import JOIN jobs job ON job.id=import.job_id WHERE import.id=?
`,
		id,
	).Scan(
		&snapshot.RootDigest,
		&snapshot.CatalogDigest,
		&snapshot.JobState,
		&snapshot.JobVersion,
		&snapshot.Execution,
		&snapshot.PendingItems,
		&snapshot.OtherActive,
	)
	if err != nil {
		return serverimport.ControlSnapshot{}, fmt.Errorf("read required import job state: %w", err)
	}
	return snapshot, nil
}

func requireControlChange(result sql.Result, err, conflict error) error {
	if err != nil {
		return fmt.Errorf("write server import control: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read server import update count: %w", err)
	}
	if changed != 1 {
		return fmt.Errorf("server import state changed: %w", conflict)
	}
	return nil
}

func (records controlRecords) evidence(
	ctx context.Context,
	before serverimport.ControlSnapshot,
	evidence serverimport.ControlEvidence,
	event, action string,
) error {
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id, scope_type, scope_id, event_type, data_json, created_at_ms) VALUES(?,
'SERVER_IMPORT', ?, ?, ?, ?)
`, before.Summary.JobID, before.Summary.ID, event, string(evidence.Event), evidence.Now); err != nil {
		return fmt.Errorf("append import control event: %w", err)
	}
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO audit_events(id, actor_kind, actor_user_id, actor_label, action, resource_type, resource_id,
before_json, after_json, diff_json, request_id, created_at_ms) VALUES(?, 'USER', ?, NULL, ?,
'SERVER_IMPORT', ?, '{}', '{}', NULL, NULL, ?)
`, evidence.AuditID, evidence.ActorID, action, before.Summary.ID, evidence.Now); err != nil {
		return fmt.Errorf("append import control audit: %w", err)
	}
	return nil
}
