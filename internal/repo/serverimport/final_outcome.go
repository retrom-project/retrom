package serverimport

import (
	"context"
	"fmt"

	"retrom/internal/repo/recordstore"
	"retrom/internal/service/serverimport"
)

func (records outcomeRecords) Final(ctx context.Context, plan serverimport.FinalOutcome) error {
	if plan.PendingState != "" {
		if _, err := recordstore.UpdateServerBiosImportItems(ctx, records.executor, recordstore.Update{
			Set: `state=?,outcome_code=?,completed_at_ms=?,updated_at_ms=?`, Scope: recordstore.Scope{
				Where: `server_import_id=? AND state IN ('PENDING','EVALUATING')`, Args: []any{plan.Unit.ImportID},
			},
			Values: []any{plan.PendingState, plan.PendingCode, plan.Now, plan.Now},
		}); err != nil {
			return fmt.Errorf("close pending import items: %w", err)
		}
	}
	counts := plan.Counts
	result, err := records.executor.ExecContext(
		ctx,
		`UPDATE server_imports SET state=?,phase=?,last_error_code=?,
 imported_matched_count=?,imported_warning_count=?,imported_missing_entry_count=?,not_found_count=?,
 skipped_existing_count=?,skipped_not_better_count=?,same_bytes_count=?,failed_item_count=?,cancelled_item_count=?,
 completed_at_ms=?,version=version+1,updated_at_ms=? WHERE id=?`,

		plan.State,
		plan.Phase,
		plan.Code,
		counts.Matched,
		counts.Warning,
		counts.Missing,
		counts.NotFound,

		counts.SkippedExisting,
		counts.SkippedNotBetter,
		counts.SameBytes,
		counts.Failed,
		counts.Cancelled,
		plan.Now,
		plan.Now,
		plan.Unit.ImportID,
	)
	if err := requireControlChange(result, err, serverimport.ErrLeaseLost); err != nil {
		return err
	}
	result, err = records.executor.ExecContext(
		ctx,
		`UPDATE jobs SET state=?,error_code=?,error_retryable=?,
 finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=?,worker_id=NULL,version=version+1,updated_at_ms=?
 WHERE id=? AND execution_no=? AND worker_id=?`,
		plan.JobState,
		plan.Code,
		plan.Retryable,
		plan.Now,
		plan.HeartbeatAt,
		plan.Now,
		plan.Unit.JobID,
		plan.Unit.Execution,
		plan.Unit.Owner,
	)
	if err := requireControlChange(result, err, serverimport.ErrLeaseLost); err != nil {
		return err
	}
	return records.event(ctx, plan.Unit, plan.EventType, plan.Event, plan.Now)
}
