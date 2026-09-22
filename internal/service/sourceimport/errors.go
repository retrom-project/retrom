package sourceimport

import "errors"

var (
	ErrNotFound        = errors.New("SOURCE_IMPORT_NOT_FOUND")
	ErrMetadataAbsent  = errors.New("PEGASUS_METADATA_NOT_FOUND")
	ErrScanLimit       = errors.New("PEGASUS_SCAN_LIMIT_EXCEEDED")
	ErrMapping         = errors.New("SOURCE_MAPPING_INCOMPLETE")
	ErrVersionConflict = errors.New("VERSION_CONFLICT")
	ErrNoSelection     = errors.New("SOURCE_NO_COLLECTION_SELECTED")
	ErrSourceChanged   = errors.New("SOURCE_SOURCE_CHANGED")
	ErrExpired         = errors.New("SOURCE_PLAN_EXPIRED")
	ErrActive          = errors.New("SOURCE_IMPORT_ACTIVE")
	ErrInvalid         = errors.New("SOURCE_IMPORT_INVALID")
	ErrNotCancellable  = errors.New("SOURCE_IMPORT_NOT_CANCELLABLE")
	ErrNotRetryable    = errors.New("SOURCE_IMPORT_NOT_RETRYABLE")
)
