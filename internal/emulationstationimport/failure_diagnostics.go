package emulationstationimport

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"retrom/internal/libraryimport"
	"retrom/internal/persistence/dberrors"
)

func sqliteFailureCause(err error) string { return dberrors.Classify(err) }

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
