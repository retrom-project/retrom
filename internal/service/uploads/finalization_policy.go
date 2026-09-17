package uploads

import (
	"errors"

	model "retrom/internal/model/uploads"
)

var ErrWorkerClosed = errors.New("UPLOAD_WORKER_CLOSED")

func finalizationFailure(cause error, deadline, now int64) (string, bool) {
	switch {
	case errors.Is(cause, errPartMissing):
		return "UPLOAD_PART_MISSING", false
	case errors.Is(cause, errPartCorrupt):
		return "UPLOAD_PART_CORRUPT", false
	case errors.Is(cause, model.ErrInputInvalid):
		return "UPLOAD_INPUT_INVALID", false
	case errors.Is(cause, model.ErrAttemptsExhausted):
		return "UPLOAD_ATTEMPTS_EXHAUSTED", true
	case deadline > 0 && now >= deadline:
		return "UPLOAD_FINALIZE_TIMEOUT", true
	default:
		return "UPLOAD_FINALIZE_IO", true
	}
}
