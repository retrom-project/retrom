package libraryimport

import (
	"context"
	"errors"

	"retrom/internal/capability/engine/scummvm"
)

var (
	ErrScummVMToolFailed   = errors.New("SCUMMVM_DETECTION_FAILED")
	ErrScummVMInputInvalid = errors.New("SCUMMVM_DETECTION_INPUT_INVALID")
	ErrScummVMLimit        = errors.New("SCUMMVM_DETECTION_LIMIT_EXCEEDED")
)

// ScummVMDetector inspects a caller-owned immutable private input tree. The
// source digest identifies its verified content manifest, not the temporary path.
type ScummVMDetector interface {
	Detect(ctx context.Context, root, sourceDigest string) (scummvm.Result, error)
}
