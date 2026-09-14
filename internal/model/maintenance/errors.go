package maintenance

import "errors"

var (
	ErrInvalidBundle    = errors.New("BACKUP_BUNDLE_INVALID")
	ErrCheckpointFailed = errors.New("BACKUP_CHECKPOINT_FAILED")
)
