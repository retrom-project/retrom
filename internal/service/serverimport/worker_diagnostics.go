package serverimport

import (
	"archive/zip"
	"errors"
	"io"
	"io/fs"

	"retrom/internal/importing"
)

type evaluationFailure struct {
	stage string
	cause error
}

func (failure *evaluationFailure) Error() string { return failure.stage + ": " + failure.cause.Error() }
func (failure *evaluationFailure) Unwrap() error { return failure.cause }

func failedEvaluation(item catalogItem, file discoveredFile, association string, err error) *evaluatedCandidate {
	state, code, stage := evaluationDiagnosis(err)
	candidate := failedCandidate(item, file, association, state)
	candidate.Details = map[string]any{"schemaVersion": 1, "code": code, "stage": stage}
	return candidate
}

func evaluationDiagnosis(err error) (string, string, string) {
	stage := "VALIDATION"
	var failure *evaluationFailure
	if errors.As(err, &failure) {
		stage = failure.stage
	}
	for _, cause := range []error{
		ErrDATUnavailable, ErrDATMachineUndefined, ErrDATEntriesUnverifiable, ErrCatalogInvalid,
	} {
		if errors.Is(err, cause) {
			return "CATALOG_INVALID", cause.Error(), "CATALOG"
		}
	}
	var pathError *fs.PathError
	if errors.As(err, &pathError) {
		return "READ_FAILED", "SERVER_IMPORT_SOURCE_UNREADABLE", stage
	}
	for _, cause := range []error{zip.ErrFormat, zip.ErrChecksum, io.ErrUnexpectedEOF} {
		if errors.Is(err, cause) {
			return "INVALID_ARCHIVE", "BIOS_ARCHIVE_INVALID", stage
		}
	}
	for _, cause := range []error{
		importing.ErrArchiveUnsafe, importing.ErrArchiveLimitExceeded,
		importing.ErrArchiveEncrypted, importing.ErrArchiveMethodUnsupported, importing.ErrArchiveCasefoldCollision,
		importing.ErrNestedArchiveUnsupported, importing.ErrArchiveResourceLimit,
	} {
		if errors.Is(err, cause) {
			return "ARCHIVE_UNSAFE", cause.Error(), stage
		}
	}
	return "VALIDATION_FAILED", "SERVER_IMPORT_VALIDATION_FAILED", stage
}
