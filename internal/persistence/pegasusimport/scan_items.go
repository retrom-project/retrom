package pegasusimport

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/pegasusimport"
)

func (records scanRecords) Items(ctx context.Context, owner application.ScanLease, items []application.ScanItem) error {
	if len(items) > 500 {
		return application.ErrScanLimit
	}
	if err := records.guard(ctx, owner); err != nil {
		return err
	}
	for _, item := range items {
		if err := records.item(ctx, owner, item); err != nil {
			return err
		}
	}
	return nil
}

func (records scanRecords) item(ctx context.Context, owner application.ScanLease, item application.ScanItem) error {
	result, err := recordstore.CreatePegasusImportItems(ctx, records.tx, `INSERT INTO pegasus_import_items(
id,import_id,collection_id,metadata_relative_path,game_ordinal,source_key,title,discovery_state,
execution_state,metadata_json,warnings_json,source_manifest_json,source_manifest_digest,discovery_code,
created_at_ms,updated_at_ms) VALUES(?,?,?,?,?,?,?,?,'PENDING',?,?,?,?,?,?,?)`, item.ID, owner.Before.ImportID,
		optionalText(item.CollectionID), item.MetadataPath, item.GameOrdinal, item.SourceKey, item.Title, item.DiscoveryState,
		item.MetadataJSON, item.WarningsJSON, item.SourceManifestJSON, item.SourceManifestDigest,
		optionalText(item.DiscoveryCode), owner.NowMS, owner.NowMS)
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return fmt.Errorf("insert Pegasus scanned item: %w", err)
	}
	if err := records.files(ctx, item, owner.NowMS); err != nil {
		return err
	}
	return records.assets(ctx, item, owner.NowMS)
}

func (records scanRecords) files(ctx context.Context, item application.ScanItem, now int64) error {
	for _, file := range item.Files {
		result, err := records.tx.ExecContext(ctx, `INSERT INTO pegasus_import_item_files(
item_id,ordinal,declared_kind,relative_path,size_bytes,source_facts_digest,state,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,'DISCOVERED',?,?)`, item.ID, file.Ordinal, file.Kind, file.Path, file.Size, file.Facts, now, now)
		if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
			return fmt.Errorf("insert Pegasus scanned file: %w", err)
		}
	}
	return nil
}

func (records scanRecords) assets(ctx context.Context, item application.ScanItem, now int64) error {
	for _, asset := range item.Assets {
		result, err := records.tx.ExecContext(ctx, `INSERT INTO pegasus_import_item_assets(
item_id,kind,resolution_method,relative_path,size_bytes,source_facts_digest,media_type,width_px,height_px,
state,created_at_ms,updated_at_ms) VALUES(?,?,?,?,?,?,?,?,?,'DISCOVERED',?,?)`, item.ID, asset.Kind, asset.Method,
			asset.Path, asset.Size, asset.Facts, asset.MediaType, asset.Width, asset.Height, now, now)
		if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
			return fmt.Errorf("insert Pegasus scanned media: %w", err)
		}
	}
	return nil
}
