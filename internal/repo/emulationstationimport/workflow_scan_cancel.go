package emulationstationimport

import (
	"context"

	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/recordstore"
)

func (records workflowRecords) cancelScanAggregate(ctx context.Context, plan application.CancellationPlan) error {
	change := recordstore.Update{
		Set: `state=?,phase=CASE WHEN ? THEN phase ELSE NULL END,
cancel_reason=?,completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Values: []any{plan.State, plan.Pending, plan.Reason, plan.CompletedAtMS, plan.NowMS},
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state='SCANNING' AND scan_job_id=? AND import_job_id IS NULL
AND source_snapshot_digest IS NULL AND scan_completed_at_ms IS NULL AND started_at_ms IS NULL`,
			Args: []any{plan.Before.Summary.ID, plan.Before.Summary.Version, plan.Before.Summary.ScanJobID},
		},
	}
	if !plan.Pending {
		change.Set += `,gamelist_count=0,invalid_gamelist_count=0,collection_count=0,folder_entry_count=0,game_count=0,
estimated_source_bytes=0,mapped_collection_count=0,skipped_collection_count=0,processable_item_count=0,
blocked_item_count=0,media_warning_count=0,discovered_cover_count=0,discovered_video_count=0`
	}
	result, err := recordstore.UpdateEmulationstationImports(ctx, records.transaction, change)
	return requireWorkflowChange(result, err, application.ErrNotCancellable)
}
