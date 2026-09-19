package launch

import (
	"encoding/json"
	"fmt"

	"retrom/internal/capability/runtime/runtimelaunch"
)

// encodeProviderValue closes a completed launch value at its assembly boundary.
func encodeProviderValue(value any) (json.RawMessage, error) {
	contents, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", runtimelaunch.ErrEnvelopeInvalid, err)
	}
	return contents, nil
}
