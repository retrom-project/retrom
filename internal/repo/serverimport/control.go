package serverimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/model/serverimport"
	"retrom/internal/repo/dbexec"

	"github.com/google/uuid"
)

type Control struct {
	database      *sql.DB
	preCommitHook func() error
}

func NewControl(database *sql.DB) *Control { return &Control{database: database} }

// WithPreCommitHook sets a function called after all writes but before commit.
// Test-only: enables fault injection for rollback verification.
func (c *Control) WithPreCommitHook(hook func() error) *Control {
	c.preCommitHook = hook
	return c
}

func (repository *Control) CommitCancel(
	ctx context.Context, cmd serverimport.CancelCommand,
) (serverimport.CancelResult, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return serverimport.CancelResult{},
			fmt.Errorf("begin server import control: %w", err)
	}
	defer dbexec.Rollback(tx)

	records := controlRecords{tx}
	before, err := records.Current(ctx, cmd.ID)
	if errors.Is(err, serverimport.ErrNotFound) {
		return serverimport.CancelResult{}, serverimport.ErrNotCancellable
	}
	if err != nil {
		return serverimport.CancelResult{},
			fmt.Errorf("read import cancellation state: %w", err)
	}

	if !serverimport.CancelValid(before, cmd.Version) {
		return serverimport.CancelResult{}, serverimport.ErrNotCancellable
	}

	evidence, err := newControlEvidence(cmd.ActorID, cmd.Now)
	if err != nil {
		return serverimport.CancelResult{}, err
	}

	plan := serverimport.Cancellation{
		Before:         before,
		Pending:        before.Summary.State == "RUNNING",
		State:          "CANCEL_REQUESTED",
		Reason:         cmd.Reason,
		CancelledItems: before.Summary.Counts.Cancelled,
		Evidence:       evidence,
	}
	if !plan.Pending {
		plan.State = "CANCELLED"
		now := evidence.Now
		plan.CompletedAt = &now
		plan.CancelledItems += before.PendingItems
	}

	if err := records.Cancel(ctx, plan); err != nil {
		return serverimport.CancelResult{},
			fmt.Errorf("cancel server import: %w", err)
	}

	after, err := records.Current(ctx, cmd.ID)
	if err != nil {
		return serverimport.CancelResult{},
			fmt.Errorf("read cancelled import: %w", err)
	}

	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(); err != nil {
			return serverimport.CancelResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return serverimport.CancelResult{},
			fmt.Errorf("commit server import control: %w", err)
	}
	return serverimport.CancelResult{
		Summary: after.Summary, Pending: plan.Pending,
	}, nil
}

func (repository *Control) CommitRetry(
	ctx context.Context, cmd serverimport.RetryCommand,
) (serverimport.Summary, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return serverimport.Summary{},
			fmt.Errorf("begin server import control: %w", err)
	}
	defer dbexec.Rollback(tx)

	records := controlRecords{tx}
	before, err := records.Current(ctx, cmd.ID)
	if errors.Is(err, serverimport.ErrNotFound) {
		return serverimport.Summary{}, serverimport.ErrNotRetryable
	}
	if err != nil {
		return serverimport.Summary{},
			fmt.Errorf("read import retry state: %w", err)
	}

	if !serverimport.Retryable(before, cmd.Version, cmd.ValidRoots) {
		return serverimport.Summary{}, serverimport.ErrNotRetryable
	}

	plan, err := newManualRetry(before, cmd.ActorID, cmd.Now)
	if err != nil {
		return serverimport.Summary{}, err
	}

	if err := records.Retry(ctx, plan); err != nil {
		return serverimport.Summary{},
			fmt.Errorf("reset server import: %w", err)
	}

	after, err := records.Current(ctx, cmd.ID)
	if err != nil {
		return serverimport.Summary{},
			fmt.Errorf("read retried import: %w", err)
	}

	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(); err != nil {
			return serverimport.Summary{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return serverimport.Summary{},
			fmt.Errorf("commit server import control: %w", err)
	}
	return after.Summary, nil
}

type controlRecords struct{ executor dbexec.Executor }

func (records controlRecords) Current(
	ctx context.Context, id string,
) (serverimport.ControlSnapshot, error) {
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
		return serverimport.ControlSnapshot{},
			fmt.Errorf("read required import job state: %w", err)
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

func newControlEvidence(
	actorID string, now int64,
) (serverimport.ControlEvidence, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return serverimport.ControlEvidence{},
			fmt.Errorf("create import audit identity: %w", err)
	}
	event := []byte(`{"schemaVersion":1}`)
	return serverimport.ControlEvidence{
		ActorID: actorID, AuditID: id.String(), Event: event, Now: now,
	}, nil
}

func newManualRetry(
	before serverimport.ControlSnapshot,
	actorID string,
	now int64,
) (serverimport.ManualRetry, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return serverimport.ManualRetry{},
			fmt.Errorf("create import retry identity: %w", err)
	}
	summary := before.Summary
	execution := before.Execution + 1
	input, err := json.Marshal(
		map[string]any{
			"schemaVersion": 1,
			"kind":          "SERVER_BIOS_IMPORT",
			"scope": map[string]any{
				"type": "SERVER_IMPORT",
				"id":   summary.ID,
			},
			"executionId": id.String(),
			"inputs": map[string]any{
				"serverImportVersion":   summary.Version,
				"rootId":                summary.Root.ID,
				"sourceRelativePath":    summary.SourceRelativePath,
				"rootConfigDigest":      before.RootDigest,
				"catalogSnapshotDigest": before.CatalogDigest,
				"replaceIfBetter":       summary.ReplaceIfBetter,
			},
		},
	)
	if err != nil {
		return serverimport.ManualRetry{},
			fmt.Errorf("encode import retry input: %w", err)
	}
	payload, err := json.Marshal(
		map[string]any{"inputExecutionNo": execution},
	)
	if err != nil {
		return serverimport.ManualRetry{},
			fmt.Errorf("encode import retry payload: %w", err)
	}
	retryEvent, err := json.Marshal(
		map[string]any{"schemaVersion": 1, "executionNo": execution},
	)
	if err != nil {
		return serverimport.ManualRetry{},
			fmt.Errorf("encode import retry event: %w", err)
	}
	evidence, err := newControlEvidence(actorID, now)
	if err != nil {
		return serverimport.ManualRetry{}, err
	}
	evidence.Event = retryEvent
	digest := sha256.Sum256(input)
	return serverimport.ManualRetry{
		Before:      before,
		Execution:   execution,
		Input:       input,
		InputDigest: hex.EncodeToString(digest[:]),
		Payload:     payload,
		Evidence:    evidence,
	}, nil
}
