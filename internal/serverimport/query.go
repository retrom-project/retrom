package serverimport

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"retrom/internal/dbexec"

	"retrom/internal/persistence/recordstore"

	"github.com/google/uuid"
)

// Cancellation item, job, aggregate, event and audit writes share one transaction.
func (service *Service) Cancel(
	ctx context.Context,
	importID string,
	version int64,
	reason, userID string,
) (Summary, bool, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return Summary{}, false, ErrNotCancellable
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Summary{}, false, fmt.Errorf("serverimport/begin cancel transaction: %w", err)
	}
	defer dbexec.Rollback(transaction)
	var state, jobID string
	var actualVersion int64
	if err := transaction.QueryRowContext(ctx, `
SELECT state,version,job_id FROM server_imports WHERE id=?
`, importID).Scan(&state, &actualVersion, &jobID); err != nil ||
		actualVersion != version ||
		state != "QUEUED" && state != "RUNNING" {
		return Summary{}, false, ErrNotCancellable
	}
	now := service.now().UnixMilli()
	pending := state == "RUNNING"
	newState := "CANCELLED"
	jobState := "CANCELLED"
	var completed any = now
	if pending {
		newState = "CANCEL_REQUESTED"
		jobState = "CANCEL_REQUESTED"
		completed = nil
	} else {
		if _, err := recordstore.UpdateServerBiosImportItems(ctx, transaction, recordstore.Update{
			Set: `
state='CANCELLED',outcome_code='CANCELLED',
completed_at_ms=?,updated_at_ms=?
`,
			Scope: recordstore.Scope{
				Where: `server_import_id=?`,
				Args:  []any{importID},
			},
			Values: []any{now, now},
		}); err != nil {
			return Summary{}, false, fmt.Errorf("serverimport/cancel queued items: %w", err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state=?,cancel_requested_at_ms=?,cancel_reason=?,finished_at_ms=?,
version=version+1,updated_at_ms=? WHERE id=?
`, jobState, now, reason, completed, now, jobID); err != nil {
		return Summary{}, false, fmt.Errorf("serverimport/cancel job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE server_imports SET state=?,cancel_requested_at_ms=?,cancel_reason=?,
cancelled_item_count=CASE WHEN ?='CANCELLED' THEN catalog_item_count ELSE cancelled_item_count END,
completed_at_ms=?,version=version+1,updated_at_ms=? WHERE id=?
`, newState, now, reason, newState, completed, now, importID); err != nil {
		return Summary{}, false, fmt.Errorf("serverimport/cancel aggregate: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'CANCEL_REQUESTED',json_object('schemaVersion',1),?)
`, jobID, importID, now); err != nil {
		return Summary{}, false, fmt.Errorf("serverimport/write cancel event: %w", err)
	}
	auditID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'USER',?,NULL,'SERVER_IMPORT_CANCEL_REQUESTED','SERVER_IMPORT',?,'{}','{}',NULL,NULL,?)
`, auditID.String(), userID, importID, now); err != nil {
		return Summary{}, false, fmt.Errorf("serverimport/write cancel audit: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Summary{}, false, fmt.Errorf("serverimport/commit cancel transaction: %w", err)
	}
	summary, err := service.Get(ctx, importID)
	return summary, pending, err
}

// Retry reset, new immutable input and audit/event writes form one transaction.
func (service *Service) Retry(ctx context.Context, importID string, version int64, userID string) (Summary, error) {
	plan, err := service.prepareRetry(ctx, importID, version)
	if err != nil {
		return Summary{}, err
	}
	if err := service.resetRetry(ctx, plan, version, userID); err != nil {
		return Summary{}, err
	}
	service.signal()
	return service.Get(ctx, importID)
}

type retryPlan struct {
	importID, storedDigest, catalogDigest, jobID string
	rootSummary                                  Summary
}

func (service *Service) prepareRetry(ctx context.Context, importID string, version int64) (retryPlan, error) {
	rootSummary, err := service.Get(ctx, importID)
	if err != nil || rootSummary.State != "FAILED" || rootSummary.Version != version ||
		rootSummary.LastErrorCode == nil ||
		(*rootSummary.LastErrorCode != "SERVER_IMPORT_ROOT_UNAVAILABLE" && *rootSummary.LastErrorCode != "INTERNAL_ERROR") {
		return retryPlan{}, ErrNotRetryable
	}
	root, ok := service.roots[rootSummary.Root.ID]
	if !ok {
		return retryPlan{}, ErrNotRetryable
	}
	var storedDigest, catalogDigest, jobID string
	if err := service.database.QueryRowContext(ctx, `
SELECT root_config_digest,catalog_snapshot_digest,job_id FROM server_imports WHERE id=?
`, importID).Scan(&storedDigest, &catalogDigest, &jobID); err != nil ||
		storedDigest != root.digest {
		return retryPlan{}, ErrNotRetryable
	}
	return retryPlan{importID, storedDigest, catalogDigest, jobID, rootSummary}, nil
}

func (service *Service) resetRetry(ctx context.Context, plan retryPlan, version int64, userID string) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("serverimport/begin retry transaction: %w", err)
	}
	defer dbexec.Rollback(transaction)
	var execution int64
	if err := transaction.QueryRowContext(ctx, `SELECT execution_no FROM jobs WHERE id=?`, plan.jobID).
		Scan(&execution); err != nil {
		return fmt.Errorf("serverimport/read retry execution number: %w", err)
	}
	execution++
	now := service.now().UnixMilli()
	inputID, _ := uuid.NewV7()
	input := map[string]any{
		"schemaVersion": 1,
		"kind":          "SERVER_BIOS_IMPORT",
		"scope":         map[string]any{"type": "SERVER_IMPORT", "id": plan.importID},
		"executionId":   inputID.String(),
		"inputs": map[string]any{
			"serverImportVersion":   version,
			"rootId":                plan.rootSummary.Root.ID,
			"sourceRelativePath":    plan.rootSummary.SourceRelativePath,
			"rootConfigDigest":      plan.storedDigest,
			"catalogSnapshotDigest": plan.catalogDigest,
			"replaceIfBetter":       plan.rootSummary.ReplaceIfBetter,
		},
	}
	encoded, _ := json.Marshal(input)
	digest := fmt.Sprintf("%x", sha256Bytes(encoded))
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,?,?,?,?)
`, plan.jobID, execution, string(encoded), digest, now); err != nil {
		return fmt.Errorf("serverimport/write retry input snapshot: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',execution_no=?,payload_json=json_object('inputExecutionNo',?),
attempt_count=0,available_at_ms=?,execution_started_at_ms=NULL,execution_deadline_at_ms=NULL,
leased_until_ms=NULL,heartbeat_at_ms=NULL,finished_at_ms=NULL,worker_id=NULL,error_code=NULL,
error_retryable=NULL,cancel_requested_at_ms=NULL,cancel_reason=NULL,version=version+1,updated_at_ms=?
WHERE id=?
`, execution, execution, now, now, plan.jobID); err != nil {
		return fmt.Errorf("serverimport/reset retry job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
DELETE FROM server_bios_import_candidates WHERE server_import_id=?
`, plan.importID); err != nil {
		return fmt.Errorf("serverimport/clear retry candidates: %w", err)
	}
	if _, err := recordstore.UpdateServerBiosImportItems(ctx, transaction, recordstore.Update{
		Set: `
state='PENDING',candidate_count=0,match_method=NULL,
selection_details_json=NULL,previous_installation_id=NULL,new_installation_id=NULL,outcome_code=NULL,
completed_at_ms=NULL,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `server_import_id=?`,
			Args:  []any{plan.importID},
		},
		Values: []any{now},
	}); err != nil {
		return fmt.Errorf("serverimport/reset retry items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE server_imports SET state='QUEUED',phase=NULL,candidate_count=0,evaluated_item_count=0,
multi_candidate_item_count=0,imported_matched_count=0,imported_warning_count=0,
imported_missing_entry_count=0,not_found_count=0,skipped_existing_count=0,skipped_not_better_count=0,
same_bytes_count=0,failed_item_count=0,cancelled_item_count=0,last_error_code=NULL,
cancel_requested_at_ms=NULL,cancel_reason=NULL,completed_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=?
`, now, plan.importID); err != nil {
		return fmt.Errorf("serverimport/reset retry aggregate: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'MANUAL_RETRY',json_object('schemaVersion',1,'executionNo',?),?)
`, plan.jobID, plan.importID, execution, now); err != nil {
		return fmt.Errorf("serverimport/write retry event: %w", err)
	}
	auditID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'USER',?,NULL,'SERVER_IMPORT_RETRIED','SERVER_IMPORT',?,'{}','{}',NULL,NULL,?)
`, auditID.String(), userID, plan.importID, now); err != nil {
		return fmt.Errorf("serverimport/write retry audit: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("serverimport/commit retry transaction: %w", err)
	}
	return nil
}

func sha256Bytes(value []byte) [32]byte { return sha256.Sum256(value) }
