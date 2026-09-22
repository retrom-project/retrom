package sourceimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/sourceimport"
)

func (records workerSettlementRecords) CompleteReview(
	ctx context.Context,
	change application.RecoveryReviewChange,
) error {
	before := change.Handoff.Before
	id := before.Identity
	warnings, err := json.Marshal(change.Handoff.Warnings)
	if err != nil {
		return fmt.Errorf("encode retained Source review warnings: %w", err)
	}
	owned := application.OwnedItem{Execution: change.Execution, Item: application.ExecutionItem{
		ID: id.ItemID, ImportID: id.ImportID, State: before.State, Version: before.Version,
	}}
	args := itemFenceArgs(owned, change.Handoff.NowMS)
	args = append(args, id.LibraryJobID, id.LibraryItemID, id.LibraryItemID, id.LibraryJobID)
	result, err := recordstore.UpdateSourceImportItems(ctx, records.tx, recordstore.Update{
		Set: `execution_state='REVIEW_PENDING',error_code=NULL,error_details_json=NULL,retryable=0,
warnings_json=?,completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Values: []any{string(warnings), change.Handoff.NowMS, change.Handoff.NowMS},
		Scope: recordstore.Scope{Where: `id=? AND import_id=? AND version=? AND execution_state=?` + itemExecutionFence + `
AND library_import_job_id=? AND library_import_item_id=?
AND EXISTS(SELECT 1 FROM import_items WHERE id=? AND import_job_id=? AND state='REVIEW_PENDING')`, Args: args},
	})
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	return RefreshCountsAndEvent(
		ctx, records.tx, change.Execution.JobID, change.Execution.ImportID, id.ItemID, "REVIEW_PENDING", change.Handoff.NowMS,
	)
}
