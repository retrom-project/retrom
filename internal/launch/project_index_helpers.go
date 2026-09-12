package launch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"retrom/internal/importing"
)

type runtimeProjectIndex struct {
	Files         []runtimeProjectIndexFile `json:"files"`
	SchemaVersion int                       `json:"schemaVersion"`
}

type runtimeProjectIndexFile struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	URL       string `json:"url"`
}

func buildRuntimeProjectIndex(
	projectRoot, markerPath string,
	files []runtimeProjectIndexFile,
	maximumFiles int,
	allowEmptyFiles bool,
	diagnosticName string,
) (ProjectIndexView, error) {
	if len(files) < 1 || len(files) > maximumFiles {
		return ProjectIndexView{}, ErrCredential
	}
	seen := make(map[string]struct{}, len(files))
	markerFound := false
	for index := range files {
		normalized, err := importing.ValidateLogicalPath(files[index].Path)
		if err != nil || normalized != files[index].Path || files[index].SizeBytes < 0 ||
			(!allowEmptyFiles && files[index].SizeBytes == 0) {
			return ProjectIndexView{}, ErrCredential
		}
		folded := importing.ASCIICaseFold(normalized)
		if _, duplicate := seen[folded]; duplicate {
			return ProjectIndexView{}, ErrCredential
		}
		seen[folded] = struct{}{}
		markerFound = markerFound || normalized == markerPath
		files[index].URL = projectRoot + escapeProjectPath(normalized)
	}
	if !markerFound {
		return ProjectIndexView{}, ErrCredential
	}
	contents, err := json.Marshal(runtimeProjectIndex{Files: files, SchemaVersion: 1})
	if err != nil {
		return ProjectIndexView{}, fmt.Errorf("marshal %s project index: %w", diagnosticName, err)
	}
	digest := sha256.Sum256(contents)
	return ProjectIndexView{Contents: contents, SHA256: hex.EncodeToString(digest[:])}, nil
}
