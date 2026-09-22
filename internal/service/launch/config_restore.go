package launch

import (
	"slices"

	"retrom/internal/runtimebundle"
)

func providerRestore(id string, restore ConfigRestore, target runtimebundle.Target) (any, bool, error) {
	if !restore.Required {
		return nil, false, nil
	}
	if !restore.Found || target.Checkpoint == nil || !slices.Contains(target.Checkpoint.ReadFormats, restore.Format) {
		return nil, false, ErrCredential
	}
	return map[string]any{
		"url": "/runtime/launches/" + id + "/state", "format": restore.Format,
		"sha256": restore.Digest, "sizeBytes": restore.Size,
	}, true, nil
}
