package maintenance

import "errors"

var (
	ErrDataRootLocked   = errors.New("DATA_ROOT_LOCKED")
	ErrInvalidBundle    = errors.New("BACKUP_BUNDLE_INVALID")
	ErrCheckpointFailed = errors.New("BACKUP_CHECKPOINT_FAILED")
)
