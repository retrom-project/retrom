package gamecontent

import "errors"

var (
	ErrInvalid                  = errors.New("GAME_CONTENT_INVALID")
	ErrIdempotencyKeyReused     = errors.New("IDEMPOTENCY_KEY_REUSED")
	ErrExecutionLost            = errors.New("GAME_CONTENT_EXECUTION_LOST")
	ErrAdminGameNotFound        = errors.New("GAME_NOT_FOUND")
	ErrAdminGameVersionConflict = errors.New("GAME_METADATA_VERSION_CONFLICT")
)

// ContentValidationError is a terminal, non-retryable publication error
// with a domain-specific code (e.g. GAME_CONTENT_UNCHANGED).
type ContentValidationError struct{ Code string }

func (err *ContentValidationError) Error() string { return err.Code }
