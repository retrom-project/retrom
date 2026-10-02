package launch

import (
	"encoding/json"
	"fmt"

	"retrom/internal/core/rpgmaker/materializer"
)

// The same immutable index supports the native lookup tree and managed content
// readers. Build from the locked files, including games imported before caching.
func marshalEasyRPGProjectIndex(files []runtimeProjectIndexFile) ([]byte, error) {
	sources := make([]materializer.SourceFile, len(files))
	for index, file := range files {
		sources[index] = materializer.SourceFile{Path: file.Path, Size: file.SizeBytes}
	}
	native, err := materializer.BuildEasyRPGIndex(sources)
	if err != nil {
		return nil, fmt.Errorf("%w: EasyRPG lookup: %w", ErrCredential, err)
	}
	var index map[string]json.RawMessage
	if err := json.Unmarshal(native.Contents, &index); err != nil {
		return nil, fmt.Errorf("decode EasyRPG lookup: %w", err)
	}
	index["schemaVersion"] = json.RawMessage("1")
	index["files"], err = json.Marshal(files)
	if err != nil {
		return nil, fmt.Errorf("marshal EasyRPG files: %w", err)
	}
	contents, err := json.Marshal(index)
	if err != nil {
		return nil, fmt.Errorf("marshal EasyRPG project index: %w", err)
	}
	return contents, nil
}
