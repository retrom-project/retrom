package pegasusimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/pegasusimport"
)

func (records scanRecords) Finish(
	ctx context.Context,
	owner application.ScanLease,
	summary application.ScanSummary,
) error {
	args := append([]any{owner.NowMS, owner.NowMS}, scanOwnerArgs(owner)...)
	result, err := records.tx.ExecContext(ctx, `UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,
heartbeat_at_ms=NULL,worker_id=NULL,error_code=NULL,error_retryable=NULL,version=version+1,updated_at_ms=?`+
		scanOwnerFence, args...)
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	s := summary.Shape
	result, err = recordstore.UpdatePegasusImports(ctx, records.tx, recordstore.Update{
		Set: `source_snapshot_digest=?,state='AWAITING_MAPPING',phase=NULL,metadata_count=?,invalid_metadata_count=?,
collection_count=?,game_count=?,estimated_source_bytes=?,processable_item_count=?,blocked_item_count=?,
media_warning_count=?,discovered_cover_count=?,discovered_video_count=?,scan_completed_at_ms=?,
version=version+1,updated_at_ms=?`,
		Values: []any{
			summary.SnapshotDigest, s.Metadata, s.InvalidMetadata, s.Collections, s.Items, s.EstimatedBytes,
			s.Items - s.Blocked, s.Blocked, summary.MediaWarnings, s.Covers, s.Videos, owner.NowMS, owner.NowMS,
		},
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state='SCANNING' AND scan_job_id=?
AND import_job_id IS NULL AND scan_completed_at_ms IS NULL`,
			Args: []any{owner.Before.ImportID, owner.Before.ImportVersion, owner.Before.JobID},
		},
	})
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	data, err := json.Marshal(struct {
		SchemaVersion   int   `json:"schemaVersion"`
		Metadata        int64 `json:"metadata"`
		Collections     int64 `json:"collections"`
		Games           int64 `json:"games"`
		InvalidMetadata int64 `json:"invalidMetadata"`
	}{1, s.Metadata, s.Collections, s.Items, s.InvalidMetadata})
	if err != nil {
		return fmt.Errorf("encode Pegasus scan success: %w", err)
	}
	result, err = records.tx.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'SUCCEEDED',?,?)`,
		owner.Before.JobID,
		owner.Before.ImportID,
		string(data),
		owner.NowMS,
	)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}
