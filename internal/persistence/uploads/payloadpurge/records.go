package payloadpurge

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"retrom/internal/persistence/recordstore"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	application "retrom/internal/service/payloadrelease"
)

type Records struct{ Executor dbapi.Executor }

const effectUploadConsumptions = `(SELECT count(*) FROM upload_consumptions consumption
 WHERE consumption.upload_session_id=file.upload_session_id
 AND (consumption.upload_file_id IS NULL OR consumption.upload_file_id=file.id) AND
consumption.released_at_ms IS NULL)`

func (records Records) Candidates(
	ctx context.Context,
	sessionID, cursor string,
	limit int,
) ([]application.EffectUpload, error) {
	query := `SELECT file.id,file.upload_session_id,COALESCE(file.final_blob_id,''),file.state,
session.state,session.version,` + effectUploadConsumptions + `
FROM upload_files file JOIN upload_sessions session ON session.id=file.upload_session_id
WHERE file.upload_session_id=? AND file.id>? AND file.state='COMPLETE'
AND ` + effectUploadConsumptions + `=0 ORDER BY file.id LIMIT ?`
	rows, err := records.Executor.QueryContext(ctx, query, sessionID, cursor, limit)
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

func (records Records) Purge(ctx context.Context, file application.EffectUpload, now int64) error {
	predicate := `upload_files.id=? AND upload_files.upload_session_id=? AND upload_files.state=? AND
upload_files.final_blob_id=?
AND EXISTS(SELECT 1 FROM upload_sessions session WHERE session.id=upload_files.upload_session_id AND
session.state=? AND session.version=?)
AND ` + strings.ReplaceAll(effectUploadConsumptions, "file.", "upload_files.") + `=?`
	result, err := recordstore.UpdateReferences(
		ctx,
		records.Executor,
		"upload_files",
		recordstore.Update{
			Set:    `state='PURGED',final_blob_id=NULL,payload_released_at_ms=?,updated_at_ms=?`,
			Values: []any{now, now},

			Scope: recordstore.Scope{
				Where: predicate,
				Args: []any{
					file.ID,
					file.SessionID,
					file.State,
					file.BlobID,
					file.SessionState,
					file.SessionVersion,
					file.ActiveConsumptions,
				},
			},
		},
	)
	if err := effectCount(result, err, 1); err != nil {
		return fmt.Errorf("purge upload reference: %w", err)
	}
	if _, err := recordstore.UpdateReferences(
		ctx,
		records.Executor,
		"import_files",
		recordstore.Update{
			Set:    "blob_id=NULL,released_at_ms=?",
			Values: []any{now},
			Scope:  recordstore.Scope{Where: "id=? AND blob_id=?", Args: []any{file.ID, file.BlobID}},
		},
	); err != nil {
		return fmt.Errorf("release received import file: %w", err)
	}
	return nil
}

func effectCount(result sql.Result, err error, expected int64) error {
	if err != nil {
		return fmt.Errorf("write upload release: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count upload release: %w", err)
	}
	if count != expected {
		return application.ErrEffectConflict
	}
	return nil
}
