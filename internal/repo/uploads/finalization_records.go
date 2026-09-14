package uploads

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	service "retrom/internal/model/uploads"
)

func (records finalizationRecords) Manifest(ctx context.Context, id string) ([]service.FrozenFile, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT file.id,file.declared_size_bytes,part.part_no,part.offset_bytes,part.size_bytes,part.sha256,part.storage_key
FROM upload_files file LEFT JOIN upload_parts part ON part.upload_file_id=file.id
WHERE file.upload_session_id=? AND file.state!='COMPLETE' ORDER BY file.id,part.part_no`, id)
	if err != nil {
		return nil, fmt.Errorf("read finalization manifest: %w", err)
	}
	defer func() { cleanup.Error("close finalization manifest", rows.Close()) }()
	return scanFinalizationManifest(rows)
}

func (records finalizationRecords) Invalidate(ctx context.Context, part service.BrokenPart, now int64) error {
	if part.Missing {
		return nil
	}
	if err := requireChange(records.executor.ExecContext(ctx, `
DELETE FROM upload_parts WHERE upload_file_id=? AND part_no=? AND sha256=? AND storage_key=? AND size_bytes=?`,
		part.FileID, part.Number, part.Part.SHA256, part.Part.Path, part.Part.Size)); err != nil {
		return err
	}
	return requireChange(records.executor.ExecContext(ctx, `
UPDATE upload_files SET received_size_bytes=received_size_bytes-?,updated_at_ms=?
WHERE id=? AND received_size_bytes>=?`, part.Part.Size, now, part.FileID, part.Part.Size))
}

func (records finalizationRecords) Count(ctx context.Context, id string) (int, error) {
	var count int
	err := records.executor.QueryRowContext(ctx, `
SELECT session.total_files-(SELECT count(*) FROM upload_files
WHERE upload_session_id=session.id AND state='COMPLETE')
FROM upload_sessions session WHERE session.id=?`, id).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count unfinished upload files: %w", err)
	}
	return count, nil
}

func (records finalizationRecords) Repair(ctx context.Context, key service.FileKey, number int) (bool, error) {
	var allowed bool
	err := records.executor.QueryRowContext(ctx, `
SELECT EXISTS(SELECT 1 FROM upload_sessions session JOIN jobs job ON job.id=session.finalize_job_id
JOIN job_events event ON event.job_id=job.id AND event.event_type='FAILED'
WHERE session.id=? AND job.state='FAILED' AND json_extract(event.data_json,'$.executionNo')=job.execution_no
AND json_extract(event.data_json,'$.failedPart.fileId')=? AND json_extract(event.data_json,'$.failedPart.partNo')=?)`,
		key.UploadID, key.FileID, number).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("read failed upload part: %w", err)
	}
	return allowed, nil
}
