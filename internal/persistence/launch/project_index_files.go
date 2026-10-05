package launch

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	application "retrom/internal/service/launch"
)

const productProjectIndexFiles = `
SELECT file.logical_name,file.format_version,((blob.value)::jsonb #>> '{sha256}'),
(((blob.value)::jsonb #>> '{size_bytes}'))::bigint,'GAME',0,0
FROM launch_content_files file JOIN LATERAL (SELECT file.file_record AS value) blob ON blob.value IS NOT NULL
WHERE file.launch_session_id=? ORDER BY file.logical_name`

const previewProjectIndexFiles = `
SELECT logical_name,format,digest,size_bytes,role,sort_order,is_primary FROM (
 SELECT preview.content_logical_name AS logical_name,preview.content_format AS format,
 ((blob.value)::jsonb #>> '{sha256}') AS digest,(((blob.value)::jsonb #>> '{size_bytes}'))::bigint AS size_bytes,
'GAME' AS role,0 AS sort_order,1 AS is_primary
 FROM runtime_preview_sessions preview JOIN LATERAL (SELECT preview.content_file_record AS value) blob ON
blob.value IS NOT NULL WHERE preview.id=?
 UNION ALL
 SELECT file.logical_name,preview.content_format,((blob.value)::jsonb #>> '{sha256}'),
(((blob.value)::jsonb #>> '{size_bytes}'))::bigint,file.role,file.sort_order,0
 FROM runtime_preview_files file JOIN LATERAL (SELECT file.file_record AS value) blob ON blob.value IS NOT NULL
 JOIN runtime_preview_sessions preview ON preview.id=file.preview_session_id
 WHERE file.preview_session_id=? AND file.role IN ('PROJECT_FILE','RUNTIME_FILE')
) ORDER BY logical_name`

func projectIndexFiles(
	ctx context.Context,
	executor dbapi.Executor,
	ref application.SessionRef,
) ([]application.ProjectIndexRecord, error) {
	query := productProjectIndexFiles
	args := []any{ref.ID}
	if ref.Preview {
		query = previewProjectIndexFiles
		args = append(args, ref.ID)
	}
	rows, err := executor.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query project index files: %w", err)
	}
	defer func() { cleanup.Error("close project index files", rows.Close()) }()
	files := make([]application.ProjectIndexRecord, 0)
	for rows.Next() {
		var record application.ProjectIndexRecord
		file := &record.Content
		if err := rows.Scan(
			&file.LogicalName,
			&file.Format,
			&file.Digest,
			&file.Size,
			&file.Role,
			&record.Order,
			&record.Primary,
		); err != nil {
			return nil, fmt.Errorf("scan project index file: %w", err)
		}
		files = append(files, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project index files: %w", err)
	}
	return files, nil
}
