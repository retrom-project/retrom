package libraryimport

import "errors"

var (
	ErrImportReadQuery    = errors.New("INVALID_IMPORT_READ_QUERY")
	ErrImportReadNotFound = errors.New("IMPORT_NOT_FOUND")
	ErrBulkNotFound       = errors.New("REVIEW_BULK_NOT_FOUND")
)
