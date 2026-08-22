package pegasusimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"retrom/internal/cleanup"
	"retrom/internal/libraryimport"
)

type executionItem struct {
	ID, TargetPlatformID, TargetPlatformKind, TargetDATVersionID, MetadataJSON string
	LibraryImportJobID, LibraryImportItemID                                    string
	TagIDs                                                                     []string
	Files                                                                      []executionFile
	Assets                                                                     []executionAsset
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
	var tagSnapshot string
	err = transaction.QueryRowContext(ctx, `
SELECT item.id,collection.target_platform_instance_id,collection.target_platform_id,
COALESCE(collection.target_dat_version_id,''),item.metadata_json,
collection.tag_snapshot_json,
COALESCE(item.library_import_job_id,''),COALESCE(item.library_import_item_id,'')
FROM pegasus_import_items item JOIN pegasus_import_collections collection ON collection.id=item.collection_id
WHERE item.import_id=? AND item.execution_state='PENDING' AND collection.mapping_action='IMPORT'
ORDER BY item.metadata_relative_path,item.game_ordinal,item.id LIMIT 1`, importID).
		Scan(
			&item.ID, &item.TargetPlatformID, &item.TargetPlatformKind,
			&item.TargetDATVersionID, &item.MetadataJSON, &tagSnapshot,
			&item.LibraryImportJobID, &item.LibraryImportItemID,
		)
	if errors.Is(err, sql.ErrNoRows) {
		return executionItem{}, false, nil
	}
	if err != nil {
		return executionItem{}, false, fmt.Errorf("pegasusimport/read next item: %w", err)
	}
	var references []struct {
		TagID string `json:"tagId"`
	}
	if err := json.Unmarshal([]byte(tagSnapshot), &references); err != nil || references == nil {
		return executionItem{}, false, ErrInvalid
	}
	for _, reference := range references {
		item.TagIDs = append(item.TagIDs, reference.TagID)
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
	if err := service.updateExecutionPhase(ctx, unit.ImportID, "COPYING_CONTENT"); err != nil {
		service.closeItemWithFailure(
			ctx, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "",
			service.itemFailure("STORAGE", "UPDATE_IMPORT_PHASE", err, firstSourcePath(item)),
		)
		return
	}
	if item.LibraryImportJobID != "" && item.LibraryImportItemID != "" {
		result, err := service.database.ExecContext(ctx, `
UPDATE pegasus_import_items SET execution_state='VALIDATING',updated_at_ms=?
WHERE id=? AND execution_state='COPYING'
AND library_import_job_id=? AND library_import_item_id=?
`, service.now().UnixMilli(), item.ID, item.LibraryImportJobID, item.LibraryImportItemID)
		if err != nil || rowsAffected(result) != 1 {
			service.closeItemWithFailure(
				ctx, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "",
				withLibraryImportIdentity(
					service.itemFailure("STORAGE", "RESUME_REVIEW_HANDOFF", err, firstSourcePath(item)),
					item.LibraryImportJobID,
					item.LibraryImportItemID,
				),
			)
			return
		}
		service.prepareLibraryReview(
			ctx,
			unit,
			item,
			item.LibraryImportJobID,
			libraryimport.ServerImportItem{ItemID: item.LibraryImportItemID},
		)
		return
	}
	if !service.copyExecutionFiles(ctx, unit, root, &item) {
		return
	}
	service.copyExecutionAssets(ctx, unit, root, &item)
	if service.importCancelled(ctx, unit.ImportID) {
		service.closeItem(ctx, item.ID, "CANCELLED", "CANCELLED", false, "")
		return
	}
	service.prepareReviewItem(ctx, unit, root, item)
}

func (service *Service) updateExecutionPhase(ctx context.Context, importID, phase string) error {
	now := service.now().UnixMilli()
	if _, err := service.database.ExecContext(ctx, `
UPDATE pegasus_imports
SET phase=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND phase IS NOT ?
`, phase, now, importID, phase); err != nil {
		return fmt.Errorf("pegasusimport/update execution phase: %w", err)
	}
	return nil
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
