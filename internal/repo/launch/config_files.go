package launch

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/launch"
)

const configProductFiles = `
SELECT logical_name,format,digest,size_bytes,role,virtual_path FROM (
 SELECT file.logical_name,file.format_version AS format,blob.sha256 AS digest,blob.size_bytes,
 'GAME' AS role,'' AS virtual_path
 FROM launch_content_files file JOIN blobs blob ON blob.id=file.blob_id
 WHERE file.launch_session_id=?
 UNION ALL
 SELECT file.logical_name,'',blob.sha256,blob.size_bytes,
 CASE WHEN file.kind='BIOS' THEN 'EXTERNAL_FILE' ELSE file.kind END,file.virtual_path
 FROM launch_external_files file JOIN blobs blob ON blob.id=file.blob_id
 WHERE file.launch_session_id=?
) WHERE (?=0 OR role='GAME')
ORDER BY role,CASE WHEN role='EXTERNAL_FILE' THEN virtual_path ELSE '' END,logical_name`

const configPreviewFiles = `
SELECT logical_name,format,digest,size_bytes,role,virtual_path FROM (
 SELECT preview.content_logical_name AS logical_name,preview.content_format AS format,
 blob.sha256 AS digest,blob.size_bytes,'GAME' AS role,'' AS virtual_path
 FROM review_preview_sessions preview JOIN blobs blob ON blob.id=preview.content_blob_id
 WHERE preview.id=?
 UNION ALL
 SELECT file.logical_name,preview.content_format,blob.sha256,blob.size_bytes,
 CASE WHEN file.role IN ('PROJECT_FILE','RUNTIME_FILE') THEN 'GAME' ELSE file.role END,
 COALESCE(file.virtual_path,'')
 FROM review_preview_files file JOIN blobs blob ON blob.id=file.blob_id
 JOIN review_preview_sessions preview ON preview.id=file.preview_session_id
 WHERE file.preview_session_id=?
) WHERE (?=0 OR role='GAME')
ORDER BY role,CASE WHEN role='EXTERNAL_FILE' THEN virtual_path ELSE '' END,logical_name`

func configFiles(
	ctx context.Context,
	executor dbexec.Executor,
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
