package libraryimport

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/launch"
)

func validateReviewPreviewFile(ctx context.Context, executor dbapi.Executor,
	plan application.PreviewCreatePlan, file application.PreviewFile,
) error {
	var query string
	var args []any
	switch file.Role {
	case "PROJECT_FILE":
		query = `SELECT EXISTS(SELECT 1 FROM import_item_source_snapshot_files
 WHERE source_snapshot_id=? AND role='PROJECT_FILE' AND logical_name=? AND file_record=?)`
		args = []any{plan.Source.SourceSnapshotID, file.LogicalName, file.FileRecord}
	case "RUNTIME_FILE":
		query = `SELECT EXISTS(SELECT 1 FROM import_item_runtime_files
 WHERE import_item_id=? AND file_record=?)`
		args = []any{plan.Request.ImportItemID, file.FileRecord}
	default:
		return nil
	}
	var valid bool
	if err := dbapi.QueryRowContext(ctx, executor, query, args...).Scan(&valid); err != nil {
		return fmt.Errorf("read review resource authority: %w", err)
	}
	if !valid {
		return fmt.Errorf("invalid review preview resource: %w", recordstore.ErrInvariant)
	}
	return nil
}
