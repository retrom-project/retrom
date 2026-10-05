package diagnostic

import (
	"encoding/json"
	"errors"
	"fmt"
)

var errSnapshotInvalid = errors.New("invalid dependency snapshot")

// WithRejection changes only the common content diagnostic. Engine-owned facts
// remain in the original snapshot, including when a new policy blocks delivery.
func WithRejection(raw string, rejection *Rejection) (string, error) {
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return "", fmt.Errorf("invalid dependency snapshot: %w", err)
	}
	if snapshot == nil {
		return "", errSnapshotInvalid
	}
	if _, exists := snapshot["contentRejection"]; !exists && rejection == nil {
		return raw, nil
	}
	delete(snapshot, "contentRejection")
	if rejection != nil {
		encoded, err := json.Marshal(rejection)
		if err != nil {
			return "", fmt.Errorf("encode content rejection: %w", err)
		}
		snapshot["contentRejection"] = encoded
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("encode content dependency snapshot: %w", err)
	}
	return string(encoded), nil
}
