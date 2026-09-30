package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	service "retrom/internal/service/libraryimport"
)

// ReadReviewRuntimeFiles selects the concrete resources for the current runtime.
// BIOS records are read from installations; companion archives belong to source snapshots.
func ReadReviewRuntimeFiles(
	ctx context.Context, executor dbapi.Executor, itemID string,
) ([]service.PreparedValidationFile, error) {
	runtime, err := ReadReviewRuntime(ctx, executor, itemID)
	if err != nil {
		return nil, err
	}
	return readReviewRuntimeFiles(ctx, executor, runtime)
}

func readReviewRuntimeFiles(
	ctx context.Context, executor dbapi.Executor, runtime ReviewRuntime,
) ([]service.PreparedValidationFile, error) {
	rows, err := executor.QueryContext(ctx, `
SELECT role,logical_name,file_record,sort_order FROM import_item_runtime_files
WHERE import_item_id=? ORDER BY role,sort_order,logical_name`, runtime.ItemID)
	if err != nil {
		return nil, fmt.Errorf("read review runtime files: %w", err)
	}
	defer func() { cleanup.Error("close current review files", rows.Close()) }()
	files := []service.PreparedValidationFile{}
	for rows.Next() {
		var file service.PreparedValidationFile
		if err := rows.Scan(&file.Role, &file.LogicalName, &file.FileRecord, &file.SortOrder); err != nil {
			return nil, fmt.Errorf("scan review runtime file: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review runtime files: %w", err)
	}
	files = append(files, runtime.Companions...)
	for _, dependency := range runtime.BIOS {
		if dependency.DeliveryKind == "BIOS_BUNDLE" && dependency.FileRecord != nil {
			files = append(files, service.PreparedValidationFile{
				Role: "BIOS_BUNDLE", LogicalName: dependency.LogicalName, FileRecord: *dependency.FileRecord,
			})
		}
	}
	return files, nil
}
