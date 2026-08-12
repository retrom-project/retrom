package pegasusimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/libraryimport"
	"retrom/internal/mediaasset"
	"retrom/internal/serversource"
)

type executionItem struct {
	ID, TargetPlatformID, TargetPlatformKind, MetadataJSON string
	Files                                                  []executionFile
	Assets                                                 []executionAsset
}

type executionFile struct {
	Ordinal     int64
	Path, Facts string
	Size        int64
	BlobID      string
}

type executionAsset struct {
	Kind, Path, Facts, MediaType string
	Size                         int64
	Width, Height                sql.NullInt64
	BlobID                       string
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-reader.ctx.Done():
		return 0, fmt.Errorf("pegasusimport/read cancelled: %w", reader.ctx.Err())
	default:
	}
	if len(buffer) > 8<<20 {
		buffer = buffer[:8<<20]
	}
	count, err := reader.reader.Read(buffer)
	if errors.Is(err, io.EOF) {
		return count, io.EOF
	}
	if err != nil {
		return count, fmt.Errorf("pegasusimport/read source: %w", err)
	}
	return count, nil
}

func (service *Service) executeImport(ctx context.Context, unit work, root Root) {
	for {
		cancelled, err := service.closeCancelled(ctx, unit)
		if err != nil {
			service.fail(ctx, unit, "INTERNAL_ERROR", true)
			return
		}
		if cancelled {
			return
		}
		item, found, err := service.nextItem(ctx, unit.ImportID)
		if err != nil {
			service.fail(ctx, unit, "INTERNAL_ERROR", true)
			return
		}
		if !found {
			if err := service.finishImport(ctx, unit); err != nil {
				service.fail(ctx, unit, "INTERNAL_ERROR", true)
			}
			return
		}
		service.processItem(ctx, unit, root, item)
	}
}

func (service *Service) nextItem(ctx context.Context, importID string) (executionItem, bool, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return executionItem{}, false, fmt.Errorf("pegasusimport/claim item transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var item executionItem
	err = transaction.QueryRowContext(ctx, `
SELECT item.id,collection.target_platform_instance_id,collection.target_platform_id,item.metadata_json
FROM pegasus_import_items item JOIN pegasus_import_collections collection ON collection.id=item.collection_id
WHERE item.import_id=? AND item.execution_state='PENDING' AND collection.mapping_action='IMPORT'
ORDER BY item.metadata_relative_path,item.game_ordinal,item.id LIMIT 1`, importID).
		Scan(&item.ID, &item.TargetPlatformID, &item.TargetPlatformKind, &item.MetadataJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return executionItem{}, false, nil
	}
	if err != nil {
		return executionItem{}, false, fmt.Errorf("pegasusimport/read next item: %w", err)
	}
	now := service.now().UnixMilli()
	result, err := transaction.ExecContext(
		ctx,
		`UPDATE pegasus_import_items SET execution_state='COPYING',updated_at_ms=? WHERE id=? AND execution_state='PENDING'`,
		now,
		item.ID,
	)
	if err != nil || rowsAffected(result) != 1 {
		return executionItem{}, false, fmt.Errorf("pegasusimport/claim item: %w", err)
	}
	item.Files, err = executionFiles(ctx, transaction, item.ID)
	if err != nil {
		return executionItem{}, false, err
	}
	item.Assets, err = executionAssets(ctx, transaction, item.ID)
	if err != nil {
		return executionItem{}, false, err
	}
	if err := transaction.Commit(); err != nil {
		return executionItem{}, false, fmt.Errorf("pegasusimport/commit item claim: %w", err)
	}
	return item, true, nil
}

func executionFiles(ctx context.Context, transaction *sql.Tx, itemID string) ([]executionFile, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT ordinal,relative_path,size_bytes,source_facts_digest
FROM pegasus_import_item_files
WHERE item_id=?
ORDER BY ordinal`, itemID)
	if err != nil {
		return nil, fmt.Errorf("pegasusimport/query item files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]executionFile, 0)
	for rows.Next() {
		var file executionFile
		if err := rows.Scan(&file.Ordinal, &file.Path, &file.Size, &file.Facts); err != nil {
			return nil, fmt.Errorf("pegasusimport/scan item file: %w", err)
		}
		result = append(result, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pegasusimport/iterate item files: %w", err)
	}
	return result, nil
}

func executionAssets(ctx context.Context, transaction *sql.Tx, itemID string) ([]executionAsset, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT kind,relative_path,size_bytes,source_facts_digest,media_type,width_px,height_px
FROM pegasus_import_item_assets
WHERE item_id=? AND state='DISCOVERED'
ORDER BY kind`, itemID)
	if err != nil {
		return nil, fmt.Errorf("pegasusimport/query item assets: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]executionAsset, 0)
	for rows.Next() {
		var asset executionAsset
		if err := rows.Scan(
			&asset.Kind, &asset.Path, &asset.Size, &asset.Facts,
			&asset.MediaType, &asset.Width, &asset.Height,
		); err != nil {
			return nil, fmt.Errorf("pegasusimport/scan item asset: %w", err)
		}
		result = append(result, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pegasusimport/iterate item assets: %w", err)
	}
	return result, nil
}

func (service *Service) processItem(ctx context.Context, unit work, root Root, item executionItem) {
	if !service.copyExecutionFiles(ctx, unit, root, &item) {
		return
	}
	service.copyExecutionAssets(ctx, unit, root, &item)
	if service.importCancelled(ctx, unit.ImportID) {
		service.closeItem(ctx, item.ID, "CANCELLED", "CANCELLED", false, "")
		return
	}
	service.publishExecutionItem(ctx, unit, root, item)
}

func (service *Service) copyExecutionFiles(
	ctx context.Context,
	unit work,
	root Root,
	item *executionItem,
) bool {
	for index := range item.Files {
		metadata, err := service.copySource(
			ctx,
			root,
			unit.RelativePath,
			item.Files[index].Path,
			item.Files[index].Size,
			item.Files[index].Facts,
		)
		if err != nil {
			code := "READ_FAILED"
			if errors.Is(err, ErrSourceChanged) {
				code = "PEGASUS_SOURCE_CHANGED"
			}
			service.closeItem(ctx, item.ID, terminalForCode(code), code, !errors.Is(err, ErrSourceChanged), "")
			return false
		}
		blobID, err := service.recordCopiedFile(ctx, item.ID, item.Files[index].Ordinal, metadata)
		if err != nil {
			service.closeItem(ctx, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "")
			return false
		}
		item.Files[index].BlobID = blobID
	}
	return true
}

func (service *Service) copyExecutionAssets(
	ctx context.Context,
	unit work,
	root Root,
	item *executionItem,
) {
	for index := range item.Assets {
		metadata, valid, err := service.copyAsset(ctx, root, unit.RelativePath, item.Assets[index])
		if err != nil || !valid {
			service.closeAssetWarning(ctx, item.ID, item.Assets[index].Kind, mediaWarning(item.Assets[index].Kind, err))
			continue
		}
		blobID, err := service.recordCopiedAsset(
			ctx, item.ID, item.Assets[index].Kind, metadata, item.Assets[index].MediaType,
		)
		if err != nil {
			service.closeAssetWarning(ctx, item.ID, item.Assets[index].Kind, "PEGASUS_MEDIA_READ_FAILED")
			continue
		}
		item.Assets[index].BlobID = blobID
	}
}

func (service *Service) importCancelled(ctx context.Context, importID string) bool {
	var aggregateState string
	err := service.database.QueryRowContext(
		ctx, `SELECT state FROM pegasus_imports WHERE id=?`, importID,
	).Scan(&aggregateState)
	return err != nil || aggregateState == "CANCEL_REQUESTED"
}

func (service *Service) publishExecutionItem(ctx context.Context, unit work, root Root, item executionItem) {
	files, err := service.executionSourceFiles(ctx, unit, root, item)
	if err != nil {
		service.closeItem(ctx, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "")
		return
	}
	mode := contentcapability.ModeStandard
	if len(item.Files) > 1 {
		mode = contentcapability.ModeMultiDiscM3UV1
	}
	result, err := service.importer.CreateServerSource(ctx, item.TargetPlatformID, mode, files)
	if err != nil {
		service.closeItem(ctx, item.ID, "BLOCKED_VALIDATION", "PEGASUS_RUNTIME_BLOCKED", false, "")
		return
	}
	imported, found := selectServerImportItem(result.Items, item.Files)
	if !found {
		service.closeItem(
			ctx, item.ID, "BLOCKED_CONTENT", "PEGASUS_CONTENT_FORMAT_UNSUPPORTED", false, "",
		)
		return
	}
	if err := service.attachLibraryResult(ctx, item.ID, result.Created.ImportJobID, imported); err != nil {
		service.closeItem(ctx, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "")
		return
	}
	if imported.ExistingGameID != "" {
		matches, _ := json.Marshal(imported.ExistingMatches)
		_, _ = service.database.ExecContext(
			ctx,
			`UPDATE pegasus_import_items SET existing_matches_json=?,updated_at_ms=? WHERE id=?`,
			string(matches),
			service.now().UnixMilli(),
			item.ID,
		)
		service.closeItem(ctx, item.ID, "SKIPPED_EXISTING", "", false, imported.ExistingGameID)
		return
	}
	if imported.State != "REVIEW_PENDING" || imported.ValidationStatus != "READY" {
		service.closeItem(ctx, item.ID, "BLOCKED_VALIDATION", "PEGASUS_RUNTIME_BLOCKED", false, "")
		return
	}
	var metadata libraryimport.ServerMetadata
	if err := json.Unmarshal([]byte(item.MetadataJSON), &metadata); err != nil {
		service.closeItem(ctx, item.ID, "BLOCKED_CONTENT", "PEGASUS_METADATA_SYNTAX_INVALID", false, "")
		return
	}
	externalAssets := projectExternalAssets(item.Assets)
	approved, err := service.importer.PublishServerItem(ctx, imported.ItemID, item.ID, metadata, externalAssets)
	if err != nil {
		service.closeItem(ctx, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "")
		return
	}
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		service.closeItem(ctx, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "")
		return
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_items
SET execution_state='PUBLISHED',published_game_id=?,error_code=NULL,retryable=0,
completed_at_ms=?,updated_at_ms=?
WHERE id=? AND execution_state='PUBLISHING'`, approved.GameID, now, now, item.ID); err != nil {
		service.closeItem(ctx, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "")
		return
	}
	if err := service.refreshCountsAndEvent(ctx, transaction, unit, item.ID, "PUBLISHED", now); err != nil ||
		transaction.Commit() != nil {
		service.closeItem(ctx, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "")
	}
}

func (service *Service) executionSourceFiles(
	ctx context.Context,
	unit work,
	root Root,
	item executionItem,
) ([]libraryimport.ServerSourceFile, error) {
	files := make([]libraryimport.ServerSourceFile, 0, len(item.Files))
	for _, file := range item.Files {
		files = append(
			files,
			libraryimport.ServerSourceFile{RelativePath: file.Path, BlobID: file.BlobID, SizeBytes: file.Size},
		)
	}
	arcadeArchive := item.TargetPlatformKind == "arcade" && len(item.Files) == 1 &&
		strings.EqualFold(path.Ext(item.Files[0].Path), ".zip")
	if !arcadeArchive {
		return files, nil
	}
	companions, err := service.arcadeCompanions(ctx, unit, root, item)
	if err != nil {
		return nil, err
	}
	return append(files, companions...), nil
}

func projectExternalAssets(assets []executionAsset) []libraryimport.ExternalAsset {
	result := make([]libraryimport.ExternalAsset, 0, len(assets))
	for _, asset := range assets {
		if asset.BlobID == "" {
			continue
		}
		var width, height *int64
		if asset.Width.Valid {
			width = int64Pointer(asset.Width.Int64)
		}
		if asset.Height.Valid {
			height = int64Pointer(asset.Height.Int64)
		}
		result = append(result, libraryimport.ExternalAsset{
			Kind: asset.Kind, BlobID: asset.BlobID, MediaType: asset.MediaType,
			WidthPX: width, HeightPX: height,
		})
	}
	return result
}

func (service *Service) recordCopiedFile(
	ctx context.Context,
	itemID string,
	ordinal int64,
	metadata blobstore.Metadata,
) (string, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("pegasusimport/start copied file transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	blobID, err := blobstore.EnsureRecord(ctx, transaction, metadata, "application/octet-stream", now)
	if err != nil {
		return "", fmt.Errorf("pegasusimport/record copied file blob: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_item_files
SET blob_id=?,state='COPIED',updated_at_ms=?
WHERE item_id=? AND ordinal=? AND state='DISCOVERED'`, blobID, now, itemID, ordinal); err != nil {
		return "", fmt.Errorf("pegasusimport/record copied file: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return "", fmt.Errorf("pegasusimport/commit copied file: %w", err)
	}
	return blobID, nil
}

func (service *Service) recordCopiedAsset(
	ctx context.Context,
	itemID, kind string,
	metadata blobstore.Metadata,
	mediaType string,
) (string, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("pegasusimport/start copied asset transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	blobID, err := blobstore.EnsureRecord(ctx, transaction, metadata, mediaType, now)
	if err != nil {
		return "", fmt.Errorf("pegasusimport/record copied asset blob: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_item_assets
SET blob_id=?,state='COPIED',updated_at_ms=?
WHERE item_id=? AND kind=? AND state='DISCOVERED'`, blobID, now, itemID, kind); err != nil {
		return "", fmt.Errorf("pegasusimport/record copied asset: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return "", fmt.Errorf("pegasusimport/commit copied asset: %w", err)
	}
	return blobID, nil
}

func selectServerImportItem(
	items []libraryimport.ServerImportItem,
	source []executionFile,
) (libraryimport.ServerImportItem, bool) {
	if len(source) == 0 {
		return libraryimport.ServerImportItem{}, false
	}
	wanted := make(map[string]struct{}, len(source))
	for _, file := range source {
		wanted[file.Path] = struct{}{}
	}
	var selected libraryimport.ServerImportItem
	found := false
	for _, item := range items {
		matches := false
		for _, relativePath := range item.SourceRelativePaths {
			if _, exists := wanted[relativePath]; exists {
				matches = true
				break
			}
		}
		if !matches {
			continue
		}
		if found {
			return libraryimport.ServerImportItem{}, false
		}
		selected, found = item, true
	}
	return selected, found
}

func (service *Service) arcadeCompanions(
	ctx context.Context,
	unit work,
	root Root,
	item executionItem,
) ([]libraryimport.ServerSourceFile, error) {
	candidates, err := service.arcadeCompanionCandidates(ctx, unit.ImportID, item)
	if err != nil {
		return nil, err
	}
	result := make([]libraryimport.ServerSourceFile, 0, len(candidates))
	for _, file := range candidates {
		metadata, err := service.copySource(ctx, root, unit.RelativePath, file.Path, file.Size, file.Facts)
		if err != nil {
			continue
		}
		transaction, err := service.database.BeginTx(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("pegasusimport/start companion transaction: %w", err)
		}
		blobID, err := blobstore.EnsureRecord(ctx, transaction, metadata, "application/zip", service.now().UnixMilli())
		if err == nil {
			err = transaction.Commit()
		} else {
			cleanup.Rollback(transaction)
		}
		if err != nil {
			return nil, fmt.Errorf("pegasusimport/record arcade companion: %w", err)
		}
		result = append(
			result,
			libraryimport.ServerSourceFile{RelativePath: file.Path, BlobID: blobID, SizeBytes: file.Size},
		)
	}
	return result, nil
}

func (service *Service) arcadeCompanionCandidates(
	ctx context.Context,
	importID string,
	item executionItem,
) ([]executionFile, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT file.relative_path,file.size_bytes,file.source_facts_digest
FROM pegasus_import_items candidate
JOIN pegasus_import_collections collection ON collection.id=candidate.collection_id
JOIN pegasus_import_item_files file ON file.item_id=candidate.id
WHERE candidate.import_id=? AND candidate.id<>? AND candidate.discovery_state='READY'
AND collection.mapping_action='IMPORT' AND collection.target_platform_instance_id=?
AND (SELECT count(*) FROM pegasus_import_item_files own WHERE own.item_id=candidate.id)=1
ORDER BY file.relative_path`, importID, item.ID, item.TargetPlatformID)
	if err != nil {
		return nil, fmt.Errorf("pegasusimport/query arcade companions: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]executionFile, 0)
	for rows.Next() {
		var file executionFile
		if err := rows.Scan(&file.Path, &file.Size, &file.Facts); err != nil {
			return nil, fmt.Errorf("pegasusimport/scan arcade companion: %w", err)
		}
		if !strings.EqualFold(path.Ext(file.Path), ".zip") {
			continue
		}
		result = append(result, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pegasusimport/iterate arcade companions: %w", err)
	}
	return result, nil
}

func terminalForCode(code string) string {
	if code == "PEGASUS_SOURCE_CHANGED" {
		return "SOURCE_CHANGED"
	}
	return "READ_FAILED"
}

func (service *Service) copySource(
	ctx context.Context,
	root Root,
	selectedPath, relativePath string,
	size int64,
	facts string,
) (blobstore.Metadata, error) {
	release, err := serversource.AcquireReader(ctx)
	if err != nil {
		return blobstore.Metadata{}, fmt.Errorf("pegasusimport/acquire source reader: %w", err)
	}
	defer release()
	handle, before, err := serversource.OpenRelativeFile(root.path, selectedPath, relativePath)
	if err != nil || before.Size() != size || serversource.FactsDigest(before) != facts {
		if handle != nil {
			cleanup.Error("close", handle.Close())
		}
		return blobstore.Metadata{}, ErrSourceChanged
	}
	metadata, putErr := service.blobs.Put(contextReader{ctx: ctx, reader: io.LimitReader(handle, size+1)})
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if putErr != nil {
		return blobstore.Metadata{}, fmt.Errorf("pegasusimport/copy source to CAS: %w", putErr)
	}
	if statErr != nil || metadata.Size != size || !serversource.SameFileFacts(before, after) ||
		serversource.FactsDigest(after) != facts {
		return blobstore.Metadata{}, ErrSourceChanged
	}
	return metadata, nil
}

func (service *Service) copyAsset(
	ctx context.Context,
	root Root,
	selectedPath string,
	asset executionAsset,
) (blobstore.Metadata, bool, error) {
	release, err := serversource.AcquireReader(ctx)
	if err != nil {
		return blobstore.Metadata{}, false, fmt.Errorf("pegasusimport/acquire asset reader: %w", err)
	}
	defer release()
	handle, before, err := serversource.OpenRelativeFile(root.path, selectedPath, asset.Path)
	if err != nil || before.Size() != asset.Size || serversource.FactsDigest(before) != asset.Facts {
		if handle != nil {
			cleanup.Error("close", handle.Close())
		}
		return blobstore.Metadata{}, false, ErrSourceChanged
	}
	valid := copiedAssetValid(handle, asset)
	if !valid {
		cleanup.Error("close", handle.Close())
		return blobstore.Metadata{}, false, nil
	}
	if _, err := handle.Seek(0, io.SeekStart); err != nil {
		cleanup.Error("close", handle.Close())
		return blobstore.Metadata{}, false, fmt.Errorf("pegasusimport/rewind asset: %w", err)
	}
	metadata, putErr := service.blobs.Put(contextReader{ctx: ctx, reader: io.LimitReader(handle, asset.Size+1)})
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if putErr != nil {
		return blobstore.Metadata{}, false, fmt.Errorf("pegasusimport/copy asset to CAS: %w", putErr)
	}
	if statErr != nil || metadata.Size != asset.Size || !serversource.SameFileFacts(before, after) ||
		serversource.FactsDigest(after) != asset.Facts {
		return blobstore.Metadata{}, false, ErrSourceChanged
	}
	return metadata, true, nil
}

func copiedAssetValid(handle io.ReadSeeker, asset executionAsset) bool {
	if asset.Kind == "COVER" {
		image, err := mediaasset.InspectImage(handle, asset.Size)
		return err == nil && image.MediaType == asset.MediaType &&
			asset.Width.Valid && asset.Height.Valid &&
			image.WidthPX == asset.Width.Int64 && image.HeightPX == asset.Height.Int64
	}
	mediaType, err := mediaasset.InspectVideo(handle, asset.Size)
	return err == nil && mediaType == asset.MediaType
}

func (service *Service) attachLibraryResult(
	ctx context.Context,
	itemID, importJobID string,
	imported libraryimport.ServerImportItem,
) error {
	now := service.now().UnixMilli()
	state := "VALIDATING"
	if imported.ExistingGameID == "" && imported.State == "REVIEW_PENDING" && imported.ValidationStatus == "READY" {
		state = "PUBLISHING"
	}
	_, err := service.database.ExecContext(
		ctx,
		`UPDATE pegasus_import_items
SET execution_state=?,content_kind=?,source_manifest_json=?,source_manifest_digest=?,
library_import_job_id=?,library_import_item_id=?,updated_at_ms=?
WHERE id=? AND execution_state='COPYING'`,
		state,
		imported.ContentKind,
		imported.SourceManifestJSON,
		imported.SourceManifestDigest,
		importJobID,
		imported.ItemID,
		now,
		itemID,
	)
	if err != nil {
		return fmt.Errorf("pegasusimport/attach library result: %w", err)
	}
	return nil
}

func (service *Service) closeItem(
	ctx context.Context,
	itemID, state, code string,
	retryable bool,
	existingGameID string,
) {
	now := service.now().UnixMilli()
	setExisting := any(nil)
	var revision any
	if existingGameID != "" {
		setExisting = existingGameID
		var value string
		if err := service.database.QueryRowContext(
			ctx, `SELECT current_content_revision_id FROM games WHERE id=?`, existingGameID,
		).Scan(&value); err == nil {
			revision = value
		}
	}
	_, _ = service.database.ExecContext(
		ctx,
		`UPDATE pegasus_import_items
SET execution_state=?,error_code=?,retryable=?,
existing_game_id=COALESCE(?,existing_game_id),
existing_content_revision_id=COALESCE(?,existing_content_revision_id),
completed_at_ms=?,updated_at_ms=?
WHERE id=? AND execution_state IN ('COPYING','VALIDATING','PUBLISHING')`,
		state,
		nullIfEmpty(code),
		boolInt(retryable),
		setExisting,
		revision,
		now,
		now,
		itemID,
	)
}

func (service *Service) closeAssetWarning(ctx context.Context, itemID, kind, code string) {
	now := service.now().UnixMilli()
	state := "READ_FAILED"
	if code == "PEGASUS_SOURCE_CHANGED" {
		state = "SOURCE_CHANGED"
	}
	_, _ = service.database.ExecContext(
		ctx,
		`UPDATE pegasus_import_item_assets SET state=?,warning_code=?,updated_at_ms=? WHERE item_id=? AND kind=?`,
		state,
		code,
		now,
		itemID,
		kind,
	)
	var encoded string
	if err := service.database.QueryRowContext(
		ctx, `SELECT warnings_json FROM pegasus_import_items WHERE id=?`, itemID,
	).Scan(&encoded); err != nil {
		return
	}
	warnings := make([]map[string]any, 0, 1)
	_ = json.Unmarshal([]byte(encoded), &warnings)
	warnings = append(warnings, map[string]any{"code": code, "field": strings.ToLower(kind)})
	encodedBytes, _ := json.Marshal(warnings)
	_, _ = service.database.ExecContext(
		ctx,
		`UPDATE pegasus_import_items SET warnings_json=?,updated_at_ms=? WHERE id=?`,
		string(encodedBytes),
		now,
		itemID,
	)
}

func mediaWarning(kind string, err error) string {
	if errors.Is(err, ErrSourceChanged) {
		return "PEGASUS_SOURCE_CHANGED"
	}
	if kind == "COVER" {
		return "PEGASUS_IMAGE_INVALID"
	}
	return "PEGASUS_VIDEO_UNSUPPORTED"
}

func (service *Service) refreshCountsAndEvent(
	ctx context.Context,
	transaction *sql.Tx,
	unit work,
	itemID, outcome string,
	now int64,
) error {
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports
SET published_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state='PUBLISHED'
),
existing_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state='SKIPPED_EXISTING'
),
blocked_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT','BLOCKED_VALIDATION')
),
failed_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')
),
cancelled_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state='CANCELLED'
),
media_warning_count=(
  SELECT count(*)
  FROM pegasus_import_items item,json_each(item.warnings_json) warning
  WHERE item.import_id=?
  AND json_extract(warning.value,'$.code') IN (
    'PEGASUS_IMAGE_INVALID','PEGASUS_VIDEO_UNSUPPORTED','PEGASUS_VIDEO_TOO_LARGE',
    'PEGASUS_MEDIA_AMBIGUOUS','PEGASUS_MEDIA_MISSING','PEGASUS_MEDIA_READ_FAILED'
  )
),
version=version+1,updated_at_ms=?
WHERE id=?`, unit.ImportID, unit.ImportID, unit.ImportID, unit.ImportID,
		unit.ImportID, unit.ImportID, now, unit.ImportID); err != nil {
		return fmt.Errorf("pegasusimport/refresh aggregate counts: %w", err)
	}
	data, _ := json.Marshal(map[string]any{"schemaVersion": 1, "itemId": itemID, "outcome": outcome})
	_, err := transaction.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'PROGRESS',?,?)`,
		unit.JobID,
		unit.ImportID,
		string(data),
		now,
	)
	if err != nil {
		return fmt.Errorf("pegasusimport/create progress event: %w", err)
	}
	return nil
}

func (service *Service) closeCancelled(ctx context.Context, unit work) (bool, error) {
	var state string
	if err := service.database.QueryRowContext(
		ctx, `SELECT state FROM pegasus_imports WHERE id=?`, unit.ImportID,
	).Scan(&state); err != nil {
		return false, fmt.Errorf("pegasusimport/read cancellation state: %w", err)
	}
	if state != "CANCEL_REQUESTED" {
		return false, nil
	}
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("pegasusimport/start cancellation close: %w", err)
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_items
SET execution_state='CANCELLED',error_code='CANCELLED',completed_at_ms=?,updated_at_ms=?
WHERE import_id=? AND execution_state='PENDING'`, now, now, unit.ImportID); err != nil {
		return false, fmt.Errorf("pegasusimport/cancel remaining items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports
SET state='CANCELLED',phase=NULL,
cancelled_item_count=(
  SELECT count(*)
  FROM pegasus_import_items
  WHERE import_id=? AND execution_state='CANCELLED'
),
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=?`, unit.ImportID, now, now, unit.ImportID); err != nil {
		return false, fmt.Errorf("pegasusimport/close cancelled import: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='CANCELLED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
version=version+1,updated_at_ms=?
WHERE id=?`, now, now, unit.JobID); err != nil {
		return false, fmt.Errorf("pegasusimport/close cancelled job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'CANCELLED','{"schemaVersion":1}',?)`,
		unit.JobID, unit.ImportID, now); err != nil {
		return false, fmt.Errorf("pegasusimport/create cancelled event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("pegasusimport/commit cancellation: %w", err)
	}
	return true, nil
}

func (service *Service) finishImport(ctx context.Context, unit work) error {
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pegasusimport/start finish transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var blocked, failed, published, existing, cancelled int64
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FILTER(
  WHERE execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT','BLOCKED_VALIDATION')
),
count(*) FILTER(
  WHERE execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')
),
count(*) FILTER(WHERE execution_state='PUBLISHED'),
count(*) FILTER(WHERE execution_state='SKIPPED_EXISTING'),
count(*) FILTER(WHERE execution_state='CANCELLED')
FROM pegasus_import_items
WHERE import_id=?`, unit.ImportID).
		Scan(&blocked, &failed, &published, &existing, &cancelled); err != nil {
		return fmt.Errorf("pegasusimport/read final counts: %w", err)
	}
	state := "COMPLETED"
	if blocked+failed > 0 {
		state = "PARTIAL_FAILURE"
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports
SET state=?,phase=NULL,published_item_count=?,existing_item_count=?,
blocked_item_count=?,failed_item_count=?,cancelled_item_count=?,retryable=?,
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=?`, state, published, existing, blocked, failed, cancelled,
		boolInt(failed > 0), now, now, unit.ImportID); err != nil {
		return fmt.Errorf("pegasusimport/finish import: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
version=version+1,updated_at_ms=?
WHERE id=?`, now, now, unit.JobID); err != nil {
		return fmt.Errorf("pegasusimport/finish job: %w", err)
	}
	data, _ := json.Marshal(
		map[string]any{
			"schemaVersion": 1,
			"state":         state,
			"published":     published,
			"existing":      existing,
			"blocked":       blocked,
			"failed":        failed,
		},
	)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'SUCCEEDED',?,?)`,
		unit.JobID, unit.ImportID, string(data), now); err != nil {
		return fmt.Errorf("pegasusimport/create success event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("pegasusimport/commit finish: %w", err)
	}
	return nil
}
