package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	variantrepository "retrom/internal/persistence/gamevariant"
	gamevariant "retrom/internal/service/gamevariant"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/launch"
)

type ProductExternals struct{ executor dbapi.Executor }

func NewProductExternals(executor dbapi.Executor) *ProductExternals {
	return &ProductExternals{executor: executor}
}

func (repository *ProductExternals) Snapshot(
	ctx context.Context,
	launchID, variantID string,
) (application.ProductExternalSnapshot, bool, error) {
	var snapshot application.ProductExternalSnapshot
	err := dbapi.QueryRowContext(ctx, repository.executor, `SELECT variant.dependency_snapshot_json,content.logical_name
FROM game_variants variant JOIN launch_content_files content ON content.launch_session_id=?
WHERE variant.id=? ORDER BY content.logical_name LIMIT 1`, launchID, variantID).Scan(
		&snapshot.DependencySnapshot,
		&snapshot.ContentName,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ProductExternalSnapshot{}, false, nil
	}
	if err != nil {
		return application.ProductExternalSnapshot{}, false, fmt.Errorf("query launch external identity: %w", err)
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT kind,virtual_path,logical_name,file_record
FROM launch_external_files WHERE launch_session_id=? ORDER BY virtual_path`, launchID)
	if err != nil {
		return application.ProductExternalSnapshot{}, false, fmt.Errorf("query locked launch external files: %w", err)
	}
	defer func() { cleanup.Error("close locked launch external files", rows.Close()) }()
	snapshot.Files = make([]application.ProductExternalFile, 0)
	for rows.Next() {
		var file application.ProductExternalFile
		if err := rows.Scan(&file.Kind, &file.VirtualPath, &file.LogicalName, &file.FileRecord); err != nil {
			return application.ProductExternalSnapshot{}, false, fmt.Errorf("scan locked launch external file: %w", err)
		}
		snapshot.Files = append(snapshot.Files, file)
	}
	if err := rows.Err(); err != nil {
		return application.ProductExternalSnapshot{}, false, fmt.Errorf("iterate locked launch external files: %w", err)
	}
	return snapshot, true, nil
}

func (repository *ProductExternals) Bundles(ctx context.Context, variantID string) ([]gamevariant.File, error) {
	files, err := variantrepository.Files(ctx, repository.executor, variantID, true)
	if err != nil {
		return nil, fmt.Errorf("read variant external files: %w", err)
	}
	return files, nil
}

func (repository *ProductExternals) Store(
	ctx context.Context,
	launchID string,
	files []application.ProductExternalFile,
	now int64,
) error {
	for _, file := range files {
		if _, err := recordstore.CreateLaunchExternalFiles(
			ctx,
			repository.executor,
			`INSERT INTO launch_external_files(
launch_session_id,virtual_path,logical_name,file_record,created_at_ms,kind) VALUES(?,?,?,?,?,?)`,
			launchID,
			file.VirtualPath,
			file.LogicalName,
			file.FileRecord,
			now,
			file.Kind,
		); err != nil {
			return fmt.Errorf("freeze launch external file: %w", err)
		}
	}
	return nil
}
