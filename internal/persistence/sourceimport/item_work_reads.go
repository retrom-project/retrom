package sourceimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	application "retrom/internal/service/sourceimport"
)

func (records itemWorkRecords) Current(ctx context.Context, id string) (application.OwnedItem, error) {
	var result application.OwnedItem
	var jobID string
	err := records.tx.QueryRowContext(ctx, `SELECT item.id,item.import_id,item.execution_state,item.version,
COALESCE(item.library_import_job_id,''),COALESCE(item.library_import_item_id,''),COALESCE(plan.import_job_id,'')
FROM source_import_items item JOIN source_imports plan ON plan.id=item.import_id WHERE item.id=?`, id).Scan(
		&result.Item.ID, &result.Item.ImportID, &result.Item.State, &result.Item.Version,
		&result.Item.LibraryImportJobID, &result.Item.LibraryImportItemID, &jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return application.OwnedItem{}, application.ErrNotFound
	}
	if err != nil {
		return application.OwnedItem{}, fmt.Errorf("read Source work item: %w", err)
	}
	result.Execution, err = records.Execution(ctx, jobID)
	if err != nil {
		return application.OwnedItem{}, err
	}
	return result, nil
}

func (records itemWorkRecords) Next(ctx context.Context, importID string) (application.ExecutionItem, bool, error) {
	var item application.ExecutionItem
	var tags string
	err := records.tx.QueryRowContext(ctx, `SELECT item.id,item.import_id,item.execution_state,item.version,
collection.target_platform_instance_id,collection.target_platform_id,COALESCE(collection.target_dat_version_id,''),
item.metadata_json,collection.tag_snapshot_json,COALESCE(item.library_import_job_id,''),
COALESCE(item.library_import_item_id,'')
FROM source_import_items item JOIN source_import_collections collection ON collection.id=item.collection_id
WHERE item.import_id=? AND item.execution_state='PENDING' AND collection.mapping_action='IMPORT'
ORDER BY item.metadata_relative_path,item.game_ordinal,item.id LIMIT 1`, importID).Scan(
		&item.ID, &item.ImportID, &item.State, &item.Version, &item.TargetPlatformID, &item.TargetPlatformKind,
		&item.TargetDATVersionID, &item.MetadataJSON, &tags, &item.LibraryImportJobID, &item.LibraryImportItemID)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ExecutionItem{}, false, nil
	}
	if err != nil {
		return application.ExecutionItem{}, false, fmt.Errorf("read next Source work item: %w", err)
	}
	var references []struct {
		TagID string `json:"tagId"`
	}
	if err := json.Unmarshal([]byte(tags), &references); err != nil {
		return application.ExecutionItem{}, false, fmt.Errorf("decode frozen Source tags: %w", err)
	}
	if references == nil {
		return application.ExecutionItem{}, false, application.ErrInvalid
	}
	item.TagIDs = make([]string, 0, len(references))
	for _, reference := range references {
		item.TagIDs = append(item.TagIDs, reference.TagID)
	}
	item.Files, err = records.files(ctx, item.ID)
	if err != nil {
		return application.ExecutionItem{}, false, err
	}
	item.Assets, err = records.assets(ctx, item.ID)
	if err != nil {
		return application.ExecutionItem{}, false, err
	}
	return item, true, nil
}

func (records itemWorkRecords) files(ctx context.Context, itemID string) ([]application.ExecutionFile, error) {
	rows, err := records.tx.QueryContext(
		ctx,
		`SELECT ordinal,relative_path,size_bytes,source_facts_digest,COALESCE(blob_id,'')
FROM source_import_item_files WHERE item_id=? ORDER BY ordinal`,
		itemID,
	)
	if err != nil {
		return nil, fmt.Errorf("query Source work files: %w", err)
	}
	defer func() { cleanup.Error("close Source work files", rows.Close()) }()
	result := []application.ExecutionFile{}
	for rows.Next() {
		var file application.ExecutionFile
		if err := rows.Scan(&file.Ordinal, &file.Path, &file.Size, &file.Facts, &file.BlobID); err != nil {
			return nil, fmt.Errorf("read Source work file: %w", err)
		}
		result = append(result, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Source work files: %w", err)
	}
	return result, nil
}

func (records itemWorkRecords) assets(ctx context.Context, itemID string) ([]application.ExecutionAsset, error) {
	rows, err := records.tx.QueryContext(
		ctx,
		`SELECT kind,relative_path,size_bytes,source_facts_digest,media_type,width_px,height_px
FROM source_import_item_assets WHERE item_id=? AND state='DISCOVERED' ORDER BY kind`,
		itemID,
	)
	if err != nil {
		return nil, fmt.Errorf("query Source work assets: %w", err)
	}
	defer func() { cleanup.Error("close Source work assets", rows.Close()) }()
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
		); err != nil {
			return nil, fmt.Errorf("read Source work asset: %w", err)
		}
		result = append(result, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Source work assets: %w", err)
	}
	return result, nil
}
