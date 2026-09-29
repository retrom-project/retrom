package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	application "retrom/internal/service/launch"
)

func previewMAMEDeviceBIOS(
	ctx context.Context, executor dbapi.Executor, targetID string,
) ([]application.PreviewFile, error) {
	if targetID != "mame-arcade" {
		return nil, nil
	}
	var fileRecord string
	err := dbapi.QueryRowContext(ctx, executor, `
SELECT installation.file_record
FROM bios_requirements requirement
JOIN bios_installations installation ON installation.requirement_id=requirement.id
WHERE requirement.provider_id='retrom-runtime' AND requirement.target_id='mame-arcade'
 AND requirement.source_kind='STATIC' AND requirement.logical_name='epr-18022.ic2'
 AND requirement.enabled=1 AND installation.is_active=1 AND installation.status='MATCHED'
LIMIT 1`).Scan(&fileRecord)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query MAME device BIOS: %w", err)
	}
	path := "/content/roms/segabill/epr-18022.ic2"
	return []application.PreviewFile{{
		Role: "EXTERNAL_FILE", LogicalName: "epr-18022.ic2",
		FileRecord: fileRecord, VirtualPath: &path,
	}}, nil
}

const previewSourceFilesSQL = `SELECT role,logical_name,file_record,NULL,sort_order
FROM import_item_source_snapshot_files
WHERE source_snapshot_id=? AND role IN ('CONTENT','DISC','PROJECT_FILE') ORDER BY sort_order,logical_name`

const previewValidationFilesSQL = `SELECT role,logical_name,file_record,NULL,sort_order
FROM import_item_validation_files WHERE import_item_core_validation_id=?
 AND role IN ('DOS_LAUNCH_BUNDLE','MULTI_DISC_PLAYLIST','RPG_EASYRPG_INDEX','RPG_MAKER_LAUNCH_BUNDLE',
 'PARENT','BIOS_BUNDLE')
ORDER BY role,sort_order,logical_name`

func previewInputFiles(
	ctx context.Context,
	executor dbapi.Executor,
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
	executor dbapi.Executor,
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

func scanPreviewCreationFile(row dbapi.Scanner) (application.PreviewFile, error) {
	var file application.PreviewFile
	if err := row.Scan(&file.Role, &file.LogicalName, &file.FileRecord, &file.VirtualPath, &file.SortOrder); err != nil {
		return application.PreviewFile{}, fmt.Errorf("scan preview file: %w", err)
	}
	return file, nil
}
