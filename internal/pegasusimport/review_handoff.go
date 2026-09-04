package pegasusimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/libraryimport"
)

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
		mode = contentcapability.ModeMultiDisc
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
