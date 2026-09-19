package emulationstationimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	model "retrom/internal/model/emulationstationimport"
	libraryimportmodel "retrom/internal/model/libraryimport"
)

func reviewSourcePath(item model.ExecutionItem) string {
	if len(item.Files) == 0 {
		return ""
	}
	return item.Files[0].Path
}

func (service *ReviewPreparer) failure(stage, operation string, cause error, path string) *model.FailureDetails {
	details := &model.FailureDetails{
		SchemaVersion: 1, Stage: stage, Operation: operation,
		CauseCode:       reviewCauseCode(cause, service.dependencies.Diagnostics),
		TechnicalDetail: service.dependencies.Diagnostics.Sanitize(cause),
	}
	if path != "" {
		details.RelativePath = &path
	}
	return details
}

func reviewCauseCode(cause error, diagnostics FailureDiagnostics) string {
	var syntax *json.SyntaxError
	switch {
	case errors.Is(cause, context.DeadlineExceeded):
		return "OPERATION_TIMEOUT"
	case errors.Is(cause, context.Canceled):
		return "OPERATION_CANCELLED"
	case errors.Is(cause, libraryimportmodel.ErrMultiDiscModeUnavailable):
		return "MULTI_DISC_MODE_UNAVAILABLE"
	case errors.Is(cause, libraryimportmodel.ErrInvalid):
		return "LIBRARY_IMPORT_INPUT_INVALID"
	case errors.As(cause, &syntax):
		return "METADATA_JSON_INVALID"
	}
	if code := diagnostics.DatabaseCause(cause); code != "" {
		return code
	}
	return "INTERNAL_OPERATION_FAILED"
}

func (service *ReviewPreparer) libraryFailure(
	cause error,
	files []libraryimportmodel.ServerSourceFile,
) *model.FailureDetails {
	path := ""
	if len(files) > 0 {
		path = files[0].RelativePath
	}
	details := service.failure("LIBRARY_IMPORT", "CREATE_SERVER_SOURCE", cause, path)
	observed, allowed := int64(len(files)), int64(libraryimportmodel.ServerSourceFileLimit)
	details.ObservedFileCount, details.AllowedFileCount = &observed, &allowed
	if errors.Is(cause, libraryimportmodel.ErrInvalid) && observed > allowed {
		details.CauseCode = "SOURCE_FILE_LIMIT_EXCEEDED"
		details.TechnicalDetail = fmt.Sprintf(
			"EmulationStation assembled %d source files for one item; library import accepts at most %d.",
			observed,
			allowed,
		)
	}
	return details
}
