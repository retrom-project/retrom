package pegasusimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/libraryimport"
	"retrom/internal/mediaasset"
	"retrom/internal/serversource"
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

func (service *Service) prepareReviewItem(ctx context.Context, unit work, root Root, item executionItem) {
	files, err := service.executionSourceFiles(ctx, unit, root, item)
	if err != nil {
		service.closeItemWithFailure(
			ctx, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "",
			service.itemFailure("SOURCE_ASSEMBLY", "ASSEMBLE_SOURCE_FILES", err, firstSourcePath(item)),
		)
		return
	}
	if err := service.updateExecutionPhase(ctx, unit.ImportID, "VALIDATING"); err != nil {
		service.closeItemWithFailure(
			ctx, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "",
			service.itemFailure("STORAGE", "UPDATE_IMPORT_PHASE", err, firstSourcePath(item)),
		)
		return
	}
	mode := contentcapability.ModeStandard
	if len(item.Files) > 1 {
		mode = contentcapability.ModeMultiDiscM3UV1
	}
	result, err := service.importer.CreateServerSource(
		ctx, item.TargetPlatformID, mode, files, item.TagIDs, unit.CreatedByUserID,
	)
	if err != nil {
		service.closeItemWithFailure(
			ctx, item.ID, "COMMIT_FAILED", "PEGASUS_LIBRARY_IMPORT_FAILED", true, "",
			service.libraryImportFailure(err, files),
		)
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
		service.closeItemWithFailure(
			ctx, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "",
			withLibraryImportIdentity(
				service.itemFailure("RESULT_ATTACHMENT", "ATTACH_LIBRARY_RESULT", err, firstSourcePath(item)),
				result.Created.ImportJobID,
				imported.ItemID,
			),
		)
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
	if imported.State != "REVIEW_PENDING" {
		service.closeItem(ctx, item.ID, "BLOCKED_CONTENT", "PEGASUS_CONTENT_FORMAT_UNSUPPORTED", false, "")
		return
	}
	service.prepareLibraryReview(ctx, unit, item, result.Created.ImportJobID, imported)
}

func (service *Service) prepareLibraryReview(
	ctx context.Context,
	unit work,
	item executionItem,
	importJobID string,
	imported libraryimport.ServerImportItem,
) {
	var metadata libraryimport.ServerMetadata
	if err := json.Unmarshal([]byte(item.MetadataJSON), &metadata); err != nil {
		service.closeItemWithFailure(
			ctx, item.ID, "BLOCKED_CONTENT", "PEGASUS_METADATA_SYNTAX_INVALID", false, "",
			withLibraryImportIdentity(
				service.itemFailure("METADATA", "DECODE_FROZEN_METADATA", err, firstSourcePath(item)),
				importJobID,
				imported.ItemID,
			),
		)
		return
	}
	if err := service.updateExecutionPhase(ctx, unit.ImportID, "PREPARING_REVIEWS"); err != nil {
		service.closeItemWithFailure(
			ctx, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "",
			withLibraryImportIdentity(
				service.itemFailure("STORAGE", "UPDATE_IMPORT_PHASE", err, firstSourcePath(item)),
				importJobID,
				imported.ItemID,
			),
		)
		return
	}
	_, metadataWarnings, err := service.importer.SeedServerReviewMetadata(ctx, imported.ItemID, metadata)
	if err != nil {
		service.closeItemWithFailure(
			ctx, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "",
			withLibraryImportIdentity(
				service.itemFailure("METADATA", "SEED_SERVER_REVIEW", err, firstSourcePath(item)),
				importJobID,
				imported.ItemID,
			),
		)
		return
	}
	service.finalizeReviewHandoff(ctx, unit, item, importJobID, imported.ItemID, metadataWarnings)
}

func (service *Service) finalizeReviewHandoff(
	ctx context.Context,
	unit work,
	item executionItem,
	importJobID, importItemID string,
	metadataWarnings []libraryimport.ServerMetadataWarning,
) {
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		service.closeItemWithFailure(
			ctx, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "",
			withLibraryImportIdentity(
				service.itemFailure("STORAGE", "START_REVIEW_HANDOFF_TRANSACTION", err, firstSourcePath(item)),
				importJobID,
				importItemID,
			),
		)
		return
	}
	defer cleanup.Rollback(transaction)
	if err := appendServerMetadataWarnings(ctx, transaction, item.ID, metadataWarnings, now); err != nil {
		cleanup.Rollback(transaction)
		service.closeItemWithFailure(
			ctx, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "",
			withLibraryImportIdentity(
				service.itemFailure("STORAGE", "APPEND_METADATA_WARNINGS", err, firstSourcePath(item)),
				importJobID,
				importItemID,
			),
		)
		return
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_items
SET execution_state='REVIEW_PENDING',error_code=NULL,retryable=0,
completed_at_ms=?,updated_at_ms=?
WHERE id=? AND execution_state='VALIDATING'`, now, now, item.ID)
	if err != nil || rowsAffected(result) != 1 {
		cleanup.Rollback(transaction)
		service.closeItemWithFailure(
			ctx, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "",
			withLibraryImportIdentity(
				service.itemFailure("STORAGE", "MARK_REVIEW_PENDING", err, firstSourcePath(item)),
				importJobID,
				importItemID,
			),
		)
		return
	}
	if err := service.refreshCountsAndEvent(ctx, transaction, unit, item.ID, "REVIEW_PENDING", now); err != nil {
		cleanup.Rollback(transaction)
		service.closeItemWithFailure(
			ctx, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "",
			withLibraryImportIdentity(
				service.itemFailure("STORAGE", "REFRESH_IMPORT_COUNTS", err, firstSourcePath(item)),
				importJobID,
				importItemID,
			),
		)
		return
	}
	if err := transaction.Commit(); err != nil {
		service.closeItemWithFailure(
			ctx, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "",
			withLibraryImportIdentity(
				service.itemFailure("STORAGE", "COMMIT_REVIEW_HANDOFF_TRANSACTION", err, firstSourcePath(item)),
				importJobID,
				importItemID,
			),
		)
	}
}

func appendServerMetadataWarnings(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	additions []libraryimport.ServerMetadataWarning,
	now int64,
) error {
	if len(additions) == 0 {
		return nil
	}
	var encoded string
	if err := transaction.QueryRowContext(
		ctx, `SELECT warnings_json FROM pegasus_import_items WHERE id=?`, itemID,
	).Scan(&encoded); err != nil {
		return fmt.Errorf("read metadata warnings: %w", err)
	}
	warnings := make([]map[string]any, 0, len(additions))
	if err := json.Unmarshal([]byte(encoded), &warnings); err != nil {
		return fmt.Errorf("decode metadata warnings: %w", err)
	}
	for _, addition := range additions {
		duplicate := false
		for _, existing := range warnings {
			if existing["code"] == addition.Code && existing["field"] == addition.Field {
				duplicate = true
				break
			}
		}
		if !duplicate {
			warnings = append(warnings, map[string]any{"code": addition.Code, "field": addition.Field})
		}
	}
	updated, err := json.Marshal(warnings)
	if err != nil {
		return fmt.Errorf("encode metadata warnings: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx, `UPDATE pegasus_import_items SET warnings_json=?,updated_at_ms=? WHERE id=?`,
		string(updated), now, itemID,
	); err != nil {
		return fmt.Errorf("update metadata warnings: %w", err)
	}
	return nil
}

func runtimeBlockCode(item libraryimport.ServerImportItem) string {
	if item.CompatibilityCode != "" {
		return item.CompatibilityCode
	}
	return "PEGASUS_RUNTIME_BLOCKED"
}

func firstSourcePath(item executionItem) string {
	if len(item.Files) == 0 {
		return ""
	}
	return item.Files[0].Path
}

func withLibraryImportIdentity(details *FailureDetails, importJobID, importItemID string) *FailureDetails {
	if importJobID != "" {
		details.LibraryImportJobID = &importJobID
	}
	if importItemID != "" {
		details.LibraryImportItemID = &importItemID
	}
	return details
}

func (service *Service) itemFailure(
	stage, operation string,
	err error,
	relativePath string,
) *FailureDetails {
	details := &FailureDetails{
		SchemaVersion:   1,
		Stage:           stage,
		Operation:       operation,
		CauseCode:       "INTERNAL_OPERATION_FAILED",
		TechnicalDetail: service.sanitizeTechnicalDetail(err),
	}
	if relativePath != "" {
		details.RelativePath = &relativePath
	}
	sqliteCause := sqliteFailureCause(err)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		details.CauseCode = "OPERATION_TIMEOUT"
	case errors.Is(err, context.Canceled):
		details.CauseCode = "OPERATION_CANCELLED"
	case errors.Is(err, libraryimport.ErrMultiDiscModeUnavailable):
		details.CauseCode = "MULTI_DISC_MODE_UNAVAILABLE"
	case errors.Is(err, libraryimport.ErrInvalid):
		details.CauseCode = "LIBRARY_IMPORT_INPUT_INVALID"
	case func() bool {
		var syntaxError *json.SyntaxError
		return errors.As(err, &syntaxError)
	}():
		details.CauseCode = "METADATA_JSON_INVALID"
	case sqliteCause != "":
		details.CauseCode = sqliteCause
	}
	return details
}

func sqliteFailureCause(err error) string {
	var sqliteError *sqlite.Error
	if !errors.As(err, &sqliteError) {
		return ""
	}
	switch sqliteError.Code() & 0xff {
	case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED:
		return "DATABASE_BUSY"
	case sqlite3.SQLITE_CONSTRAINT:
		return "DATABASE_CONSTRAINT_FAILED"
	default:
		return ""
	}
}

func (service *Service) libraryImportFailure(
	err error,
	files []libraryimport.ServerSourceFile,
) *FailureDetails {
	relativePath := ""
	if len(files) > 0 {
		relativePath = files[0].RelativePath
	}
	details := service.itemFailure("LIBRARY_IMPORT", "CREATE_SERVER_SOURCE", err, relativePath)
	observed, allowed := int64(len(files)), int64(libraryimport.ServerSourceFileLimit)
	details.ObservedFileCount = &observed
	details.AllowedFileCount = &allowed
	if errors.Is(err, libraryimport.ErrInvalid) && len(files) > libraryimport.ServerSourceFileLimit {
		details.CauseCode = "SOURCE_FILE_LIMIT_EXCEEDED"
		details.TechnicalDetail = fmt.Sprintf(
			"Pegasus assembled %d source files for one item; library import accepts at most %d.",
			len(files), libraryimport.ServerSourceFileLimit,
		)
	}
	return details
}

func (service *Service) sanitizeTechnicalDetail(err error) string {
	if err == nil {
		return ""
	}
	detail := err.Error()
	var pathError *os.PathError
	if errors.As(err, &pathError) && pathError.Path != "" {
		detail = strings.ReplaceAll(detail, pathError.Path, "[path]")
	}
	var linkError *os.LinkError
	if errors.As(err, &linkError) {
		if linkError.Old != "" {
			detail = strings.ReplaceAll(detail, linkError.Old, "[path]")
		}
		if linkError.New != "" {
			detail = strings.ReplaceAll(detail, linkError.New, "[path]")
		}
	}
	for _, root := range service.roots {
		if root.path != "" {
			detail = strings.ReplaceAll(detail, root.path, "[server-root]")
		}
	}
	detail = strings.Join(strings.Fields(detail), " ")
	if len(detail) > 2048 {
		detail = detail[:2048]
	}
	return detail
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
	if item.TargetDATVersionID == "" || len(item.Files) != 1 {
		return []executionFile{}, nil
	}
	machine := strings.TrimSuffix(path.Base(item.Files[0].Path), path.Ext(item.Files[0].Path))
	dependencies, err := service.arcadeDependencyMachines(ctx, item.TargetDATVersionID, machine)
	if err != nil {
		return nil, err
	}
	if len(dependencies) == 0 {
		return []executionFile{}, nil
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT file.relative_path,file.size_bytes,file.source_facts_digest
FROM pegasus_import_items candidate
JOIN pegasus_import_collections collection ON collection.id=candidate.collection_id
JOIN pegasus_import_item_files file ON file.item_id=candidate.id
WHERE candidate.import_id=? AND candidate.id<>? AND candidate.discovery_state='READY'
AND collection.mapping_action='IMPORT' AND collection.target_platform_instance_id=?
AND collection.target_dat_version_id=?
AND (SELECT count(*) FROM pegasus_import_item_files own WHERE own.item_id=candidate.id)=1
ORDER BY file.relative_path`, importID, item.ID, item.TargetPlatformID, item.TargetDATVersionID)
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
		candidateMachine := strings.TrimSuffix(path.Base(file.Path), path.Ext(file.Path))
		if _, required := dependencies[candidateMachine]; !required {
			continue
		}
		result = append(result, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pegasusimport/iterate arcade companions: %w", err)
	}
	return result, nil
}

func (service *Service) arcadeDependencyMachines(
	ctx context.Context,
	datVersionID, machine string,
) (map[string]struct{}, error) {
	rows, err := service.database.QueryContext(ctx, `
WITH RECURSIVE dependency(machine) AS (
 SELECT cloneof FROM dat_machines
 WHERE dat_version_id=? AND machine_name=? AND cloneof IS NOT NULL
 UNION
 SELECT romof FROM dat_machines
 WHERE dat_version_id=? AND machine_name=? AND romof IS NOT NULL
 UNION
 SELECT relation.cloneof FROM dat_machines relation
 JOIN dependency current ON relation.machine_name=current.machine
 WHERE relation.dat_version_id=? AND relation.cloneof IS NOT NULL
 UNION
 SELECT relation.romof FROM dat_machines relation
 JOIN dependency current ON relation.machine_name=current.machine
 WHERE relation.dat_version_id=? AND relation.romof IS NOT NULL
)
SELECT machine FROM dependency WHERE machine<>? ORDER BY machine`,
		datVersionID, machine, datVersionID, machine, datVersionID, datVersionID, machine,
	)
	if err != nil {
		return nil, fmt.Errorf("pegasusimport/query arcade dependency closure: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make(map[string]struct{})
	for rows.Next() {
		var dependency string
		if err := rows.Scan(&dependency); err != nil {
			return nil, fmt.Errorf("pegasusimport/scan arcade dependency closure: %w", err)
		}
		result[dependency] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pegasusimport/iterate arcade dependency closure: %w", err)
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
	result, err := service.database.ExecContext(
		ctx,
		`UPDATE pegasus_import_items
SET execution_state='VALIDATING',content_kind=?,source_manifest_json=?,source_manifest_digest=?,
library_import_job_id=?,library_import_item_id=?,updated_at_ms=?
WHERE id=? AND execution_state='COPYING'`,
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
	if rowsAffected(result) != 1 {
		return fmt.Errorf("pegasusimport/attach library result: %w", errItemStateChanged)
	}
	return nil
}

func (service *Service) closeItem(
	ctx context.Context,
	itemID, state, code string,
	retryable bool,
	existingGameID string,
) {
	service.closeItemWithFailure(ctx, itemID, state, code, retryable, existingGameID, nil)
}

func (service *Service) closeItemWithFailure(
	ctx context.Context,
	itemID, state, code string,
	retryable bool,
	existingGameID string,
	failure *FailureDetails,
) {
	now := service.now().UnixMilli()
	var encodedFailure any
	if failure != nil {
		if encoded, err := json.Marshal(failure); err == nil {
			encodedFailure = string(encoded)
		}
	}
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
error_details_json=?,
existing_game_id=COALESCE(?,existing_game_id),
existing_content_revision_id=COALESCE(?,existing_content_revision_id),
completed_at_ms=?,updated_at_ms=?
WHERE id=? AND execution_state IN ('COPYING','VALIDATING','PUBLISHING')`,
		state,
		nullIfEmpty(code),
		boolInt(retryable),
		encodedFailure,
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
SET review_pending_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state='REVIEW_PENDING'
),
published_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state='PUBLISHED'
),
review_discarded_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state='REVIEW_DISCARDED'
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
WHERE id=?`, unit.ImportID, unit.ImportID, unit.ImportID, unit.ImportID, unit.ImportID,
		unit.ImportID, unit.ImportID, unit.ImportID, now, unit.ImportID); err != nil {
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
	var blocked, failed, reviewPending, published, reviewDiscarded, existing, cancelled int64
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FILTER(
  WHERE execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT','BLOCKED_VALIDATION')
),
count(*) FILTER(
  WHERE execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')
),
count(*) FILTER(WHERE execution_state='REVIEW_PENDING'),
count(*) FILTER(WHERE execution_state='PUBLISHED'),
count(*) FILTER(WHERE execution_state='REVIEW_DISCARDED'),
count(*) FILTER(WHERE execution_state='SKIPPED_EXISTING'),
count(*) FILTER(WHERE execution_state='CANCELLED')
FROM pegasus_import_items
WHERE import_id=?`, unit.ImportID).
		Scan(&blocked, &failed, &reviewPending, &published, &reviewDiscarded, &existing, &cancelled); err != nil {
		return fmt.Errorf("pegasusimport/read final counts: %w", err)
	}
	state := "COMPLETED"
	if blocked+failed > 0 {
		state = "PARTIAL_FAILURE"
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports
SET state=?,phase=NULL,review_pending_item_count=?,published_item_count=?,
review_discarded_item_count=?,existing_item_count=?,
blocked_item_count=?,failed_item_count=?,cancelled_item_count=?,retryable=?,
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=?`, state, reviewPending, published, reviewDiscarded, existing, blocked, failed, cancelled,
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
			"schemaVersion":   1,
			"state":           state,
			"reviewPending":   reviewPending,
			"published":       published,
			"reviewDiscarded": reviewDiscarded,
			"existing":        existing,
			"blocked":         blocked,
			"failed":          failed,
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
