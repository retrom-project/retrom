package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/launch"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
)

type ProductExternals struct{ executor dbexec.Executor }

func NewProductExternals(executor dbexec.Executor) *ProductExternals {
	return &ProductExternals{executor: executor}
}

func (repository *ProductExternals) Snapshot(
	ctx context.Context,
	launchID, variantID string,
) (application.ProductExternalSnapshot, bool, error) {
	var snapshot application.ProductExternalSnapshot
	err := repository.executor.QueryRowContext(ctx, `SELECT variant.dependency_snapshot_json,content.logical_name
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
	rows, err := repository.executor.QueryContext(ctx, `SELECT kind,virtual_path,logical_name,blob_id
FROM launch_external_files WHERE launch_session_id=? ORDER BY virtual_path`, launchID)
	if err != nil {
		return application.ProductExternalSnapshot{}, false, fmt.Errorf("query locked launch external files: %w", err)
	}
	defer func() { cleanup.Error("close locked launch external files", rows.Close()) }()
	snapshot.Files = make([]application.ProductExternalFile, 0)
	for rows.Next() {
		var file application.ProductExternalFile
		if err := rows.Scan(&file.Kind, &file.VirtualPath, &file.LogicalName, &file.BlobID); err != nil {
			return application.ProductExternalSnapshot{}, false, fmt.Errorf("scan locked launch external file: %w", err)
		}
		snapshot.Files = append(snapshot.Files, file)
	}
	if err := rows.Err(); err != nil {
		return application.ProductExternalSnapshot{}, false, fmt.Errorf("iterate locked launch external files: %w", err)
	}
	return snapshot, true, nil
}

func (repository *ProductExternals) Bundles(ctx context.Context, variantID string) ([]application.ProductFile, error) {
	return productCreationFiles(ctx, repository.executor, variantID, true)
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
launch_session_id,virtual_path,logical_name,blob_id,created_at_ms,kind) VALUES(?,?,?,?,?,?)`,
			launchID,
			file.VirtualPath,
			file.LogicalName,
			file.BlobID,
			now,
			file.Kind,
		); err != nil {
			return fmt.Errorf("freeze launch external file: %w", err)
		}
	}
	return nil
}
