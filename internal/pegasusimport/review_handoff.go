package pegasusimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	repository "retrom/internal/persistence/pegasusimport"
	libraryservice "retrom/internal/service/libraryimport"
	application "retrom/internal/service/pegasusimport"

	"retrom/internal/persistence/recordstore"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

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
	result, err := service.importer.CreateServerSourceOnce(
		ctx, "SERVER_PEGASUS_IMPORT:"+item.ID, item.TargetPlatformID, mode, files, item.TagIDs, unit.CreatedByUserID,
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
		_, _ = recordstore.UpdatePegasusImportItems(ctx, service.database, recordstore.Update{
			Set: `existing_matches_json=?,updated_at_ms=?`,
			Scope: recordstore.Scope{
				Where: `id=?`,
				Args:  []any{item.ID},
			},
			Values: []any{string(matches), service.now().UnixMilli()},
		})
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
	ctx context.Context, unit work, item executionItem, importJobID string, imported libraryimport.ServerImportItem,
) {
	handoff := application.NewReviewHandoff(repository.NewReviewHandoff(service.database),
		libraryservice.NewMetadataSeeder(nil, service.now), service.now)
	err := handoff.Complete(ctx, application.ReviewHandoffRequest{
		ItemID: item.ID, ImportID: unit.ImportID,
		JobID: unit.JobID, LibraryJobID: importJobID, LibraryItemID: imported.ItemID,
		ExecutionNo: unit.ExecutionNo, Attempt: unit.Attempt,
	})
	if err == nil || errors.Is(err, application.ErrVersionConflict) {
		return
	}
	service.closeItemWithFailure(
		ctx,
		item.ID,
		"COMMIT_FAILED",
		"INTERNAL_ERROR",
		true,
		"",

		withLibraryImportIdentity(
			service.itemFailure("REVIEW_HANDOFF", "COMPLETE_REVIEW_HANDOFF", err, firstSourcePath(item)),
			importJobID,
			imported.ItemID,
		),
	)
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
