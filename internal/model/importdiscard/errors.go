package importdiscard

import "errors"

var (
	ErrInvalid        = errors.New("IMPORT_BATCH_DISCARD_INVALID")
	ErrNotFound       = errors.New("IMPORT_BATCH_DISCARD_NOT_FOUND")
	ErrReleaseFailed  = errors.New("IMPORT_BATCH_DISCARD_RELEASE_FAILED")
	ErrAmbiguousOwner = errors.New("IMPORT_BATCH_DISCARD_OWNER_AMBIGUOUS")
	ErrNotCancellable = errors.New("IMPORT_BATCH_DISCARD_NOT_CANCELLABLE")
)
