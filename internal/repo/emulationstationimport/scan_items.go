package emulationstationimport

import (
	"context"
	"fmt"

	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
	application "retrom/internal/service/emulationstationimport"
)

func (records scanRecords) Items(
	ctx context.Context,
	change application.ScanMutation,
	items []application.ScanItem,
) error {
	if len(items) > 500 {
		return application.ErrInvalid
	}
	if err := records.fence(ctx, change); err != nil {
		return err
	}
	for _, item := range items {
		if err := insertScannedItem(ctx, records.executor, change.Before.ImportID, item, change.NowMS); err != nil {
			return err
		}
	}
	return nil
}

func insertScannedItem(
	ctx context.Context,
	batch dbexec.Executor,
	importID string,
	item application.ScanItem,
	now int64,
) error {
	writeResult, err := recordstore.CreateEmulationstationImportItems(ctx, batch, `
INSERT INTO emulationstation_import_items(
id,import_id,collection_id,gamelist_relative_path,game_ordinal,source_key,title,
source_flags_json,discovery_state,execution_state,content_kind,metadata_json,
warnings_json,source_manifest_json,source_manifest_digest,discovery_code,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,'PENDING',?,?,?,?,?,?,?,?)`, item.ID, importID,
		item.CollectionID, item.GamelistPath, item.GameOrdinal, item.SourceKey,
		item.Title, item.SourceFlagsJSON, item.DiscoveryState, item.ContentKind,
		item.MetadataJSON, item.WarningsJSON,
		item.SourceManifestJSON, item.SourceManifestDigest, optionalText(item.DiscoveryCode), now, now)
	if err := requireWorkflowChange(writeResult, err, application.ErrVersionConflict); err != nil {
		return fmt.Errorf("emulationstationimport/insert item: %w", err)
	}
	for _, file := range item.Files {
		writeResult, err := recordstore.CreateEmulationstationImportItemFiles(ctx, batch, `
INSERT INTO emulationstation_import_item_files(
item_id,ordinal,declared_kind,relative_path,size_bytes,source_facts_digest,
state,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,'DISCOVERED',?,?)`, item.ID, file.Ordinal, file.Kind,
			file.Path, file.Size, file.Facts, now, now)
		if err := requireWorkflowChange(writeResult, err, application.ErrVersionConflict); err != nil {
			return fmt.Errorf("emulationstationimport/insert item file: %w", err)
		}
	}
	for _, asset := range item.Assets {
		writeResult, err := recordstore.CreateEmulationstationImportItemAssets(ctx, batch, `
INSERT INTO emulationstation_import_item_assets(
item_id,kind,resolution_method,relative_path,size_bytes,source_facts_digest,
media_type,width_px,height_px,state,warning_code,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, item.ID, asset.Kind, asset.Method,
			asset.Path, asset.Size, asset.Facts, asset.MediaType, asset.Width, asset.Height,
			asset.State, optionalText(asset.WarningCode), now, now)
		if err := requireWorkflowChange(writeResult, err, application.ErrVersionConflict); err != nil {
			return fmt.Errorf("emulationstationimport/insert asset: %w", err)
		}
	}
	return nil
}
