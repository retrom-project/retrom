package emulationstationimport

import (
	"context"
	"encoding/json"
	"fmt"

	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/recordstore"
)

func (records scanRecords) Complete(
	ctx context.Context,
	change application.ScanMutation,
	value application.ScanProjection,
) error {
	if err := records.fence(ctx, change); err != nil {
		return err
	}
	result, err := recordstore.UpdateEmulationstationImports(ctx, records.executor, recordstore.Update{
		Set: `source_snapshot_digest=?,state='AWAITING_MAPPING',phase=NULL,
gamelist_count=?,invalid_gamelist_count=?,collection_count=?,folder_entry_count=?,game_count=?,
estimated_source_bytes=?,processable_item_count=?,blocked_item_count=?,
media_warning_count=?,discovered_cover_count=?,discovered_video_count=?,
scan_completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Values: []any{
			value.SnapshotDigest, len(value.Gamelists), value.InvalidGamelists, len(value.Collections),
			value.FolderEntries, len(value.Items), value.EstimatedBytes, int64(len(value.Items)) - value.Blocked,
			value.Blocked, value.MediaWarnings, value.Covers, value.Videos, change.NowMS, change.NowMS,
		},
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state='SCANNING'`,
			Args:  []any{change.Before.ImportID, change.Before.ImportVersion},
		},
	})
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	result, err = records.executor.ExecContext(ctx, `UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,
leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,error_code=NULL,error_retryable=NULL,
version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING' AND version=?`,
		change.NowMS, change.NowMS, change.Before.JobID, change.Before.JobVersion)
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	encoded, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "gamelists": len(value.Gamelists), "collections": len(value.Collections),
		"games": len(value.Items), "invalidGamelists": value.InvalidGamelists,
	})
	if err != nil {
		return fmt.Errorf("encode EmulationStation scan completion: %w", err)
	}
	result, err = records.executor.ExecContext(ctx, `INSERT INTO job_events(
job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'EMULATIONSTATION_IMPORT',?,'SUCCEEDED',?,?)`,
		change.Before.JobID, change.Before.ImportID, string(encoded), change.NowMS)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}

func (records scanRecords) Reject(
	ctx context.Context,
	change application.ScanMutation,
	value application.ScanProjection,
) error {
	if err := records.fence(ctx, change); err != nil {
		return err
	}
	result, err := recordstore.UpdateEmulationstationImports(ctx, records.executor, recordstore.Update{
		Set: `gamelist_count=?,invalid_gamelist_count=?,
collection_count=0,folder_entry_count=0,game_count=0,estimated_source_bytes=0,
processable_item_count=0,blocked_item_count=0,media_warning_count=0,
discovered_cover_count=0,discovered_video_count=0,version=version+1,updated_at_ms=?`,
		Values: []any{len(value.Gamelists), value.InvalidGamelists, change.NowMS},
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state='SCANNING'`,
			Args:  []any{change.Before.ImportID, change.Before.ImportVersion},
		},
	})
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}
