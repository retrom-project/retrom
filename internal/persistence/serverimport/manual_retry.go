package serverimport

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/serverimport"
)

func (records controlRecords) Retry(ctx context.Context, plan serverimport.ManualRetry) error {
	before := plan.Before
	now := plan.Evidence.Now
	result, err := records.executor.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED', execution_no=?, payload_json=?, attempt_count=0, available_at_ms=?,
execution_started_at_ms=NULL, execution_deadline_at_ms=NULL, leased_until_ms=NULL, heartbeat_at_ms=NULL,
finished_at_ms=NULL, worker_id=NULL, error_code=NULL, error_retryable=NULL, cancel_requested_at_ms=NULL,
cancel_reason=NULL, version=version+1, updated_at_ms=? WHERE id=? AND version=? AND execution_no=? AND
state='FAILED'
`, plan.Execution, string(plan.Payload), now, now, before.Summary.JobID, before.JobVersion, before.Execution)
	if err := requireControlChange(result, err, serverimport.ErrNotRetryable); err != nil {
		return err
	}
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id, execution_no, input_json, input_digest, created_at_ms) VALUES(?,
?, ?, ?, ?)
`, before.Summary.JobID, plan.Execution, string(plan.Input), plan.InputDigest, now); err != nil {
		return fmt.Errorf("insert retry input snapshot: %w", err)
	}
	if _, err := records.executor.ExecContext(
		ctx,
		`DELETE FROM server_bios_import_candidates WHERE server_import_id=?`,
		before.Summary.ID,
	); err != nil {
		return fmt.Errorf("clear retry candidates: %w", err)
	}
	if _, err := recordstore.UpdateServerBiosImportItems(
		ctx,
		records.executor,
		recordstore.Update{
			Set: `
state='PENDING', candidate_count=0, match_method=NULL, selection_details_json=NULL,
previous_installation_id=NULL, new_installation_id=NULL, outcome_code=NULL, completed_at_ms=NULL,
updated_at_ms=?
`,
			Scope: recordstore.Scope{
				Where: `server_import_id=?`,
				Args: []any{
					before.Summary.ID,
				},
			},
			Values: []any{
				now,
			},
		},
	); err != nil {
		return fmt.Errorf("reset retry items: %w", err)
	}
	result, err = records.executor.ExecContext(ctx, `
UPDATE server_imports SET state='QUEUED', phase=NULL, candidate_count=0, evaluated_item_count=0,
multi_candidate_item_count=0, imported_matched_count=0, imported_warning_count=0,
imported_missing_entry_count=0, not_found_count=0, skipped_existing_count=0, skipped_not_better_count=0,
same_bytes_count=0, failed_item_count=0, cancelled_item_count=0, last_error_code=NULL,
cancel_requested_at_ms=NULL, cancel_reason=NULL, completed_at_ms=NULL, version=version+1, updated_at_ms=?
WHERE id=? AND version=? AND state='FAILED' AND root_config_digest=? AND catalog_snapshot_digest=?
`, now, before.Summary.ID, before.Summary.Version, before.RootDigest, before.CatalogDigest)
	if err := requireControlChange(result, err, serverimport.ErrNotRetryable); err != nil {
		return err
	}
	return records.evidence(ctx, before, plan.Evidence, "MANUAL_RETRY", "SERVER_IMPORT_RETRIED")
}
