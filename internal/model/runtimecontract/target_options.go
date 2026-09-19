package runtimecontract

import (
	"encoding/json"
	"errors"

	"retrom/internal/capability/runtime/runtimejson"
)

// ErrManifestInvalid identifies an invalid Provider manifest or schema.
var ErrManifestInvalid = errors.New("RUNTIME_PROVIDER_MANIFEST_INVALID")

// TargetOptionsSchema stores the Provider API's bounded schema dialect as JSON
// values. Successful decoding normalizes each child through the strict parser.
type TargetOptionsSchema map[string]json.RawMessage

func (schema *TargetOptionsSchema) UnmarshalJSON(contents []byte) error {
	value, err := runtimejson.DecodeTargetOptionsSchema(contents)
	if err != nil {
		return ErrManifestInvalid
	}
	*schema = value
	return nil
}
