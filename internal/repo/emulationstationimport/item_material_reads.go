package emulationstationimport

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
)

func loadExecutionFiles(ctx context.Context, executor dbexec.Executor, id string) ([]application.ExecutionFile, error) {
	rows, err := executor.QueryContext(
		ctx,
		`SELECT ordinal,relative_path,size_bytes,source_facts_digest,state,COALESCE(blob_id,'')
FROM emulationstation_import_item_files WHERE item_id=? ORDER BY ordinal`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("query EmulationStation execution files: %w", err)
	}
	defer func() { cleanup.Error("close EmulationStation execution files", rows.Close()) }()
	result := []application.ExecutionFile{}
	for rows.Next() {
		var file application.ExecutionFile
		if err := rows.Scan(&file.Ordinal, &file.Path, &file.Size, &file.Facts, &file.State, &file.BlobID); err != nil {
			return nil, fmt.Errorf("read EmulationStation execution file: %w", err)
		}
		result = append(result, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate EmulationStation execution files: %w", err)
	}
	return result, nil
}

func loadExecutionAssets(
	ctx context.Context,
	executor dbexec.Executor,
	id string,
) ([]application.ExecutionAsset, error) {
	rows, err := executor.QueryContext(
		ctx,
		`SELECT
kind,relative_path,size_bytes,source_facts_digest,media_type,width_px,height_px,state,COALESCE(blob_id,'')
FROM emulationstation_import_item_assets WHERE item_id=? AND state='DISCOVERED' ORDER BY kind`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("query EmulationStation execution assets: %w", err)
	}
	defer func() { cleanup.Error("close EmulationStation execution assets", rows.Close()) }()
	result := []application.ExecutionAsset{}
	for rows.Next() {
		var asset application.ExecutionAsset
		if err := rows.Scan(
			&asset.Kind,
			&asset.Path,
			&asset.Size,
			&asset.Facts,
			&asset.MediaType,
			&asset.Width,
			&asset.Height,
			&asset.State,
			&asset.BlobID,
		); err != nil {
			return nil, fmt.Errorf("read EmulationStation execution asset: %w", err)
		}
		result = append(result, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate EmulationStation execution assets: %w", err)
	}
	return result, nil
}
