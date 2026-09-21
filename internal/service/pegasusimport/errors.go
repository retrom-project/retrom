package pegasusimport

import "errors"

var (
	ErrNotFound        = errors.New("PEGASUS_IMPORT_NOT_FOUND")
	ErrMetadataAbsent  = errors.New("PEGASUS_METADATA_NOT_FOUND")
	ErrScanLimit       = errors.New("PEGASUS_SCAN_LIMIT_EXCEEDED")
	ErrMapping         = errors.New("PEGASUS_MAPPING_INCOMPLETE")
	ErrVersionConflict = errors.New("VERSION_CONFLICT")
	ErrNoSelection     = errors.New("PEGASUS_NO_COLLECTION_SELECTED")
	ErrSourceChanged   = errors.New("PEGASUS_SOURCE_CHANGED")
	ErrExpired         = errors.New("PEGASUS_PLAN_EXPIRED")
	ErrActive          = errors.New("PEGASUS_IMPORT_ACTIVE")
	ErrInvalid         = errors.New("PEGASUS_IMPORT_INVALID")
	ErrNotCancellable  = errors.New("PEGASUS_IMPORT_NOT_CANCELLABLE")
	ErrNotRetryable    = errors.New("PEGASUS_IMPORT_NOT_RETRYABLE")
)
