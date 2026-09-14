package gamecontent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/model/gamecontent"
)

func (records records) Upload(ctx context.Context, id string) (gamecontent.Upload, error) {
	var result gamecontent.Upload
	err := records.executor.QueryRowContext(ctx, `SELECT state,source_type,
 (SELECT count(*) FROM upload_files WHERE upload_session_id=upload_sessions.id AND state='COMPLETE'),
 (SELECT count(*) FROM upload_consumptions WHERE upload_session_id=upload_sessions.id)
 FROM upload_sessions WHERE id=?`, id).Scan(&result.State, &result.SourceType, &result.FileCount, &result.Consumptions)
	if errors.Is(err, sql.ErrNoRows) {
		return result, gamecontent.ErrInvalid
	}
	if err != nil {
		return result, fmt.Errorf("read replacement upload: %w", err)
	}
	return result, nil
}

func (records records) Input(ctx context.Context, id string, execution int64) (gamecontent.StoredInput, error) {
	var result gamecontent.StoredInput
	err := records.executor.QueryRowContext(
		ctx,
		`SELECT input.input_json,input.input_digest FROM job_input_snapshots input
 JOIN jobs job ON job.id=input.job_id
 WHERE input.job_id=? AND input.execution_no=? AND job.kind='GAME_CONTENT_REPLACE'`,
		id,
		execution,
	).
		Scan(&result.Contents, &result.Digest)
	if err != nil {
		return result, fmt.Errorf("read stored replacement execution: %w", err)
	}
	return result, nil
}

func (records records) Identity(ctx context.Context, id string) ([]gamecontent.IdentityFile, error) {
	rows, err := records.executor.QueryContext(ctx, `SELECT file.role,blob.sha256 FROM game_files file
 JOIN blobs blob ON blob.id=file.blob_id WHERE file.game_id=? ORDER BY file.sort_order,file.role,file.logical_name`, id)
	if err != nil {
		return nil, fmt.Errorf("read current content identity: %w", err)
	}
	defer func() { cleanup.Error("close content identity", rows.Close()) }()
	var result []gamecontent.IdentityFile
	for rows.Next() {
		var file gamecontent.IdentityFile
		if err := rows.Scan(&file.Role, &file.SHA256); err != nil {
			return nil, fmt.Errorf("scan content identity: %w", err)
		}
		result = append(result, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate content identity: %w", err)
	}
	return result, nil
}
