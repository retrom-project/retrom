package pegasusimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	library "retrom/internal/service/libraryimport"
)

// ReviewPreparationError retains the permanent identities already returned by source creation.
type ReviewPreparationError struct {
	LibraryJobID, LibraryItemID string
	Cause                       error
}

func (failure *ReviewPreparationError) Error() string { return failure.Cause.Error() }

func (failure *ReviewPreparationError) Unwrap() error { return failure.Cause }

func DescribeFailure(
	diagnostics FailureDiagnostics, stage, operation string, err error, relativePath string,
) *FailureDetails {
	details := &FailureDetails{
		SchemaVersion: 1, Stage: stage, Operation: operation, CauseCode: "INTERNAL_OPERATION_FAILED",
		TechnicalDetail: diagnostics.Sanitize(err),
	}
	if relativePath != "" {
		details.RelativePath = &relativePath
	}
	var preparation *ReviewPreparationError
	if errors.As(err, &preparation) {
		details.LibraryImportJobID = &preparation.LibraryJobID
		details.LibraryImportItemID = &preparation.LibraryItemID
	}
	var syntaxError *json.SyntaxError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		details.CauseCode = "OPERATION_TIMEOUT"
	case errors.Is(err, context.Canceled):
		details.CauseCode = "OPERATION_CANCELLED"
	case errors.Is(err, library.ErrMultiDiscModeUnavailable):
		details.CauseCode = "MULTI_DISC_MODE_UNAVAILABLE"
	case errors.Is(err, library.ErrInvalid):
		details.CauseCode = "LIBRARY_IMPORT_INPUT_INVALID"
	case errors.As(err, &syntaxError):
		details.CauseCode = "METADATA_JSON_INVALID"
	default:
		if cause := diagnostics.DatabaseCause(err); cause != "" {
			details.CauseCode = cause
		}
	}
	return details
}

func LibraryFailureDetails(
	diagnostics FailureDiagnostics, err error, files []library.ServerSourceFile,
) *FailureDetails {
	relativePath := ""
	if len(files) > 0 {
		relativePath = files[0].RelativePath
	}
	details := DescribeFailure(diagnostics, "LIBRARY_IMPORT", "CREATE_SERVER_SOURCE", err, relativePath)
	observed, allowed := int64(len(files)), int64(library.ServerSourceFileLimit)
	details.ObservedFileCount, details.AllowedFileCount = &observed, &allowed
	if errors.Is(err, library.ErrInvalid) && len(files) > library.ServerSourceFileLimit {
		details.CauseCode = "SOURCE_FILE_LIMIT_EXCEEDED"
		details.TechnicalDetail = fmt.Sprintf(
			"Pegasus assembled %d source files for one item; library import accepts at most %d.", len(files), allowed,
		)
	}
	return details
}
