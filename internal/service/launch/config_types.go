package launch

import (
	"encoding/json"
	"slices"

	"retrom/internal/capability/runtime/runtimelaunch"
)

// Config holds the single closed-validated Provider envelope.
type Config struct{ contents json.RawMessage }

func (configuration Config) MarshalJSON() ([]byte, error) {
	if len(configuration.contents) == 0 {
		return nil, runtimelaunch.ErrEnvelopeInvalid
	}
	return slices.Clone(configuration.contents), nil
}
