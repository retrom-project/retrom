package payloadrelease

import "fmt"

// EffectError keeps the stable domain error code available to callers at any
// layer without making the model package depend on service orchestration.
type EffectError struct {
	CodeValue string
	Cause     error
}

func (err EffectError) Error() string {
	if err.Cause == nil {
		return err.CodeValue
	}
	return fmt.Sprintf("%s: %v", err.CodeValue, err.Cause)
}

func (err EffectError) Code() string  { return err.CodeValue }
func (err EffectError) Unwrap() error { return err.Cause }

func NewEffectError(code string, cause error) error {
	return EffectError{CodeValue: code, Cause: cause}
}

var ErrEffectConflict = NewEffectError("PAYLOAD_RELEASE_SCOPE_VERSION_MISMATCH", nil)
