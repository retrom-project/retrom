package emulationstationimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	persistence "retrom/internal/persistence/emulationstationimport"
	library "retrom/internal/service/libraryimport"

	"retrom/internal/persistence/dberrors"

	"retrom/internal/contentcapability"
	"retrom/internal/libraryimport"
	application "retrom/internal/service/emulationstationimport"
)

func (service *Service) prepareReviewItem(ctx context.Context, unit work, root Root, item executionItem) error {
	files, err := service.executionSourceFiles(ctx, unit, root, item)
	if err != nil {
		if errors.Is(err, errImportCancelled) {
			return nil
		}
		return service.closeItemWithFailure(
			ctx, unit, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true,
			service.itemFailure("SOURCE_ASSEMBLY", "ASSEMBLE_SOURCE_FILES", err, firstSourcePath(item)),
		)
	}
	if err := service.updateExecutionPhase(ctx, unit, "VALIDATING"); err != nil {
		return service.closeItemWithFailure(
			ctx, unit, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true,
			service.itemFailure("STORAGE", "UPDATE_IMPORT_PHASE", err, firstSourcePath(item)),
		)
	}
	mode := contentcapability.ModeStandard
	if item.ContentKind == contentcapability.ModeMultiDisc {
		mode = contentcapability.ModeMultiDisc
	}
	result, err := service.importer.CreateServerSourceOnce(
		ctx,
		"SERVER_EMULATIONSTATION_IMPORT:"+item.ID,
		item.TargetPlatformID,
		mode,
		files,
		item.TagIDs,
		unit.CreatedByUserID,
	)
	if err != nil {
		if errors.Is(err, libraryimport.ErrMultiDiscModeUnavailable) {
			return service.closeItem(ctx, unit, item.ID, "BLOCKED_CONTENT", "MULTI_DISC_MODE_UNAVAILABLE", false)
		}
		return service.closeItemWithFailure(
			ctx, unit, item.ID, "COMMIT_FAILED", "EMULATIONSTATION_LIBRARY_IMPORT_FAILED", true,
			service.libraryImportFailure(err, files),
		)
	}
	imported, found := selectServerImportItem(result.Items, item.Files)
	if !found {
		return service.closeItem(
			ctx, unit, item.ID, "BLOCKED_CONTENT", "EMULATIONSTATION_CONTENT_FORMAT_UNSUPPORTED", false,
		)
	}
	if err := service.attachLibraryResult(ctx, item.ID, result.Created.ImportJobID, imported); err != nil {
		return service.closeItemWithFailure(
			ctx, unit, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true,
			withLibraryImportIdentity(
				service.itemFailure("RESULT_ATTACHMENT", "ATTACH_LIBRARY_RESULT", err, firstSourcePath(item)),
				result.Created.ImportJobID,
				imported.ItemID,
			),
		)
	}
	if imported.ExistingGameID != "" {
		matches := make([]application.ExistingMatch, 0, len(imported.ExistingMatches))
		for _, match := range imported.ExistingMatches {
			matches = append(matches, application.ExistingMatch{GameID: match.GameID})
		}
		return service.finishItemOutcome(
			ctx,
			unit,
			item.ID,
			application.ItemOutcome{
				State:           "SKIPPED_EXISTING",
				ExistingGameID:  imported.ExistingGameID,
				ExistingMatches: matches,
			},
		)
	}

	if imported.State != "REVIEW_PENDING" {
		return service.closeItem(
			ctx,
			unit,
			item.ID,
			"BLOCKED_CONTENT",
			"EMULATIONSTATION_CONTENT_FORMAT_UNSUPPORTED",
			false,
		)
	}
	return service.prepareLibraryReview(ctx, unit, item, result.Created.ImportJobID, imported)
}

func (service *Service) prepareLibraryReview(
	ctx context.Context,
	unit work,
	item executionItem,
	importJobID string,
	imported libraryimport.ServerImportItem,
) error {
	var metadata libraryimport.ServerMetadata
	if err := json.Unmarshal([]byte(item.MetadataJSON), &metadata); err != nil {
		return service.closeItemWithFailure(
			ctx, unit, item.ID, "BLOCKED_CONTENT", "EMULATIONSTATION_METADATA_SYNTAX_INVALID", false,
			withLibraryImportIdentity(
				service.itemFailure("METADATA", "DECODE_FROZEN_METADATA", err, firstSourcePath(item)),
				importJobID,
				imported.ItemID,
			),
		)
	}
	if err := service.updateExecutionPhase(ctx, unit, "PREPARING_REVIEWS"); err != nil {
		return service.closeItemWithFailure(
			ctx, unit, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true,
			withLibraryImportIdentity(
				service.itemFailure("STORAGE", "UPDATE_IMPORT_PHASE", err, firstSourcePath(item)),
				importJobID,
				imported.ItemID,
			),
		)
	}
	if err := service.reviewHandoff().Complete(ctx, application.ReviewHandoffRequest{
		Execution: unit, ItemID: item.ID, LibraryJobID: importJobID, LibraryItemID: imported.ItemID,
	}); err != nil {
		return fmt.Errorf("prepare EmulationStation library review: %w", err)
	}
	return nil
}

func (service *Service) reviewHandoff() *application.ReviewHandoff {
	return application.NewReviewHandoff(persistence.NewReviewHandoff(service.database),
		library.NewMetadataSeeder(nil, service.now), service.now)
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

func sqliteFailureCause(err error) string { return dberrors.Classify(err) }

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
			"EmulationStation assembled %d source files for one item; library import accepts at most %d.",
			len(files), libraryimport.ServerSourceFileLimit,
		)
	}
	return details
}

func (service *Service) sanitizeTechnicalDetail(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return "operation timed out"
	case errors.Is(err, context.Canceled):
		return "operation was cancelled"
	case errors.Is(err, libraryimport.ErrMultiDiscModeUnavailable):
		return "multi-disc import is unavailable for the selected target"
	case errors.Is(err, libraryimport.ErrInvalid):
		return "library import rejected the assembled source"
	}
	if cause := sqliteFailureCause(err); cause != "" {
		return "database operation failed: " + cause
	}
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		return "source filesystem operation failed"
	}
	var linkError *os.LinkError
	if errors.As(err, &linkError) {
		return "source filesystem link operation failed"
	}
	var syntaxError *json.SyntaxError
	if errors.As(err, &syntaxError) {
		return "metadata JSON was invalid"
	}
	return "internal operation failed"
}
