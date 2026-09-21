package payloadrelease

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	application "retrom/internal/service/payloadrelease"
)

const effectUploadReferences = `(SELECT count(*) FROM import_item_source_files WHERE upload_file_id=file.id)+
(SELECT count(*) FROM import_item_source_snapshot_files WHERE upload_file_id=file.id)+
(SELECT count(*) FROM import_item_multidisc_entries WHERE upload_file_id=file.id)+
(SELECT count(*) FROM review_uploaded_assets WHERE upload_file_id=file.id)+
(SELECT count(*) FROM review_arcade_parent_attachments WHERE upload_file_id=file.id)`

const effectUploadConsumptions = `(SELECT count(*) FROM upload_consumptions consumption
 WHERE consumption.upload_session_id=file.upload_session_id
 AND (consumption.upload_file_id IS NULL OR consumption.upload_file_id=file.id) AND
consumption.released_at_ms IS NULL)`

func (records effectRecords) Candidates(
	ctx context.Context,
	sessionID, cursor string,
	limit int,
) ([]application.EffectUpload, error) {
	query := `SELECT file.id,file.upload_session_id,COALESCE(file.final_blob_id,''),file.state,
session.state,session.version,` + effectUploadConsumptions + `,` + effectUploadReferences + `
FROM upload_files file JOIN upload_sessions session ON session.id=file.upload_session_id
WHERE file.upload_session_id=? AND file.id>? ORDER BY file.id LIMIT ?`
	rows, err := records.executor.QueryContext(ctx, query, sessionID, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("read upload purge candidates: %w", err)
	}
	defer func() { cleanup.Error("close upload candidates", rows.Close()) }()
	result := make([]application.EffectUpload, 0)
	for rows.Next() {
		var file application.EffectUpload
		if err := rows.Scan(
			&file.ID,
			&file.SessionID,
			&file.BlobID,
			&file.State,
			&file.SessionState,
			&file.SessionVersion,
			&file.ActiveConsumptions,
			&file.DomainReferences,
		); err != nil {
			return nil, fmt.Errorf("decode upload purge candidate: %w", err)
		}
		result = append(result, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upload purge candidates: %w", err)
	}
	return result, nil
}

func (records effectRecords) Purge(ctx context.Context, file application.EffectUpload, now int64) error {
	query := `UPDATE upload_files AS file SET state='PURGED',final_blob_id=NULL,payload_released_at_ms=?,updated_at_ms=?
WHERE file.id=? AND file.upload_session_id=? AND file.state=? AND file.final_blob_id=?
AND EXISTS(SELECT 1 FROM upload_sessions session WHERE session.id=file.upload_session_id AND
session.state=? AND session.version=?)
AND ` + effectUploadConsumptions + `=? AND (` + effectUploadReferences + `)=?`
	result, err := records.executor.ExecContext(
		ctx,
		query,
		now,
		now,
		file.ID,
		file.SessionID,
		file.State,
		file.BlobID,
		file.SessionState,
		file.SessionVersion,
		file.ActiveConsumptions,
		file.DomainReferences,
	)
	if err := effectCount(result, err, 1); err != nil {
		return fmt.Errorf("purge upload reference: %w", err)
	}
	return nil
}
