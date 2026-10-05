package launch

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	application "retrom/internal/service/launch"
)

const configProductFiles = `
SELECT logical_name,format,digest,size_bytes,role,virtual_path,file_record FROM (
 SELECT file.logical_name,file.format_version AS format,((blob.value)::jsonb #>> '{sha256}') AS digest,
(((blob.value)::jsonb #>> '{size_bytes}'))::bigint AS size_bytes,
 'GAME' AS role,'' AS virtual_path,file.file_record AS file_record
 FROM launch_content_files file JOIN LATERAL (SELECT file.file_record AS value) blob ON blob.value IS NOT NULL
 WHERE file.launch_session_id=?
 UNION ALL
 SELECT file.logical_name,'',((blob.value)::jsonb #>> '{sha256}'),(((blob.value)::jsonb #>> '{size_bytes}'))::bigint,
 CASE WHEN file.kind='BIOS' THEN 'EXTERNAL_FILE' ELSE file.kind END,file.virtual_path,file.file_record
 FROM launch_external_files file JOIN LATERAL (SELECT file.file_record AS value) blob ON blob.value IS NOT NULL
 WHERE file.launch_session_id=?
) WHERE (?=0 OR role='GAME')
ORDER BY role,CASE WHEN role='EXTERNAL_FILE' THEN virtual_path ELSE '' END,logical_name`

const configPreviewFiles = `
SELECT logical_name,format,digest,size_bytes,role,virtual_path,file_record FROM (
 SELECT preview.content_logical_name AS logical_name,preview.content_format AS format,
 ((blob.value)::jsonb #>> '{sha256}') AS digest,(((blob.value)::jsonb #>> '{size_bytes}'))::bigint AS size_bytes,
'GAME' AS role,'' AS virtual_path,preview.content_file_record AS file_record
 FROM runtime_preview_sessions preview JOIN LATERAL (SELECT preview.content_file_record AS value) blob ON
blob.value IS NOT NULL
 WHERE preview.id=?
 UNION ALL
 SELECT file.logical_name,preview.content_format,((blob.value)::jsonb #>> '{sha256}'),
(((blob.value)::jsonb #>> '{size_bytes}'))::bigint,
 CASE WHEN file.role IN ('PROJECT_FILE','RUNTIME_FILE') THEN 'GAME' ELSE file.role END,
 COALESCE(file.virtual_path,''),file.file_record
 FROM runtime_preview_files file JOIN LATERAL (SELECT file.file_record AS value) blob ON blob.value IS NOT NULL
 JOIN runtime_preview_sessions preview ON preview.id=file.preview_session_id
 WHERE file.preview_session_id=?
) WHERE (?=0 OR role='GAME')
ORDER BY role,CASE WHEN role='EXTERNAL_FILE' THEN virtual_path ELSE '' END,logical_name`

func configFiles(
	ctx context.Context,
	executor dbapi.Executor,
	ref application.SessionRef,
	projectOnly bool,
) ([]application.ConfigFile, error) {
	query := configProductFiles
	if ref.Preview {
		query = configPreviewFiles
	}
	rows, err := executor.QueryContext(ctx, query, ref.ID, ref.ID, projectOnly)
	if err != nil {
		return nil, fmt.Errorf("query config files: %w", err)
	}
	defer func() { cleanup.Error("close config files", rows.Close()) }()
	files := make([]application.ConfigFile, 0)
	for rows.Next() {
		var file application.ConfigFile
		if err := rows.Scan(
			&file.LogicalName,
			&file.Format,
			&file.Digest,
			&file.Size,
			&file.Role,
			&file.VirtualPath,
			&file.FileRecord,
		); err != nil {
			return nil, fmt.Errorf("scan config file: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate config files: %w", err)
	}
	return files, nil
}
