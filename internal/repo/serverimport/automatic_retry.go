package serverimport

import (
	"context"
	"fmt"

	"retrom/internal/repo/recordstore"
	"retrom/internal/service/serverimport"
)

func (records outcomeRecords) Retry(ctx context.Context, plan serverimport.AutomaticRetry) error {
	if _, err := records.executor.ExecContext(
		ctx,
		`DELETE FROM server_bios_import_candidates WHERE server_import_id=?`,
		plan.Unit.ImportID,
	); err != nil {
		return fmt.Errorf("clear retry candidates: %w", err)
	}
	if _, err := recordstore.UpdateServerBiosImportItems(ctx, records.executor, recordstore.Update{
		Set: `state='PENDING',candidate_count=0,match_method=NULL,selection_details_json=NULL,
 previous_installation_id=NULL,new_installation_id=NULL,outcome_code=NULL,completed_at_ms=NULL,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `server_import_id=?`, Args: []any{plan.Unit.ImportID}}, Values: []any{plan.Now},
	}); err != nil {
		return fmt.Errorf("reset retry items: %w", err)
	}
	result, err := records.executor.ExecContext(ctx, `UPDATE server_imports SET state='QUEUED',phase=NULL,
 candidate_count=0,evaluated_item_count=0,multi_candidate_item_count=0,skipped_special_count=0,
 skipped_unrepresentable_path_count=0,last_error_code=NULL,version=version+1,updated_at_ms=?
 WHERE id=? AND state='RUNNING'`, plan.Now, plan.Unit.ImportID)
	if err := requireControlChange(result, err, serverimport.ErrLeaseLost); err != nil {
		return err
	}
	result, err = records.executor.ExecContext(ctx, `UPDATE jobs SET state='QUEUED',available_at_ms=?,
 leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,error_code=NULL,error_retryable=NULL,
 finished_at_ms=NULL,version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
 AND execution_no=? AND worker_id=?`, plan.AvailableAt, plan.Now, plan.Unit.JobID, plan.Unit.Execution, plan.Unit.Owner)
	if err := requireControlChange(result, err, serverimport.ErrLeaseLost); err != nil {
		return err
	}
	return records.event(ctx, plan.Unit, "RETRY_SCHEDULED", plan.Event, plan.Now)
}
