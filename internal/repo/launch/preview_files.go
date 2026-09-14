package launch

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/launch"
	"retrom/internal/repo/dbexec"
)

const previewSourceFilesSQL = `SELECT role,logical_name,blob_id,NULL,sort_order
FROM import_item_source_snapshot_files
WHERE source_snapshot_id=? AND role IN ('CONTENT','DISC','PROJECT_FILE') ORDER BY sort_order,logical_name`

const previewValidationFilesSQL = `SELECT role,logical_name,blob_id,NULL,sort_order
FROM import_item_validation_files WHERE import_item_core_validation_id=?
 AND role IN ('DOS_LAUNCH_BUNDLE','MULTI_DISC_PLAYLIST','RPG_EASYRPG_INDEX','RPG_MAKER_LAUNCH_BUNDLE',
 'PARENT','BIOS_BUNDLE')
ORDER BY role,sort_order,logical_name`

func previewInputFiles(
	ctx context.Context,
	executor dbexec.Executor,
	id string,
	validation bool,
) ([]application.PreviewFile, error) {
	query := previewSourceFilesSQL
	if validation {
		query = previewValidationFilesSQL
	}
	return previewCreationFiles(ctx, executor, query, id)
}

func previewCreationFiles(
	ctx context.Context,
	executor dbexec.Executor,
	query, id string,
) ([]application.PreviewFile, error) {
	rows, err := executor.QueryContext(ctx, query, id)
	if err != nil {
		return nil, fmt.Errorf("query preview files: %w", err)
	}
	defer func() { cleanup.Error("close preview files", rows.Close()) }()
	files := make([]application.PreviewFile, 0)
	for rows.Next() {
		file, err := scanPreviewCreationFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate preview files: %w", err)
	}
	return files, nil
}

func scanPreviewCreationFile(row dbexec.Scanner) (application.PreviewFile, error) {
	var file application.PreviewFile
	if err := row.Scan(&file.Role, &file.LogicalName, &file.BlobID, &file.VirtualPath, &file.SortOrder); err != nil {
		return application.PreviewFile{}, fmt.Errorf("scan preview file: %w", err)
	}
	return file, nil
}
