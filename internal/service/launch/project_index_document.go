package launch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	model "retrom/internal/model/launch"
	"strings"

	"retrom/internal/capability/format/importing"
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

type onsProjectIndex struct {
	Files         []runtimeProjectIndexFile `json:"files"`
	FontPath      string                    `json:"fontPath"`
	SchemaVersion int                       `json:"schemaVersion"`
	Title         string                    `json:"title"`
}

func buildProjectIndexDocument(
	root, title string,
	policy projectIndexPolicy,
	files []runtimeProjectIndexFile,
) (model.ProjectIndexView, error) {
	if len(files) < policy.minimum || len(files) > policy.maximum || (policy.font != "" && len(title) > 500) {
		return model.ProjectIndexView{}, model.ErrCredential
	}
	seen := make(map[string]struct{}, len(files))
	markerFound, fontFound := false, policy.font == ""
	for index := range files {
		normalized, err := importing.ValidateLogicalPath(files[index].Path)
		if err != nil || normalized != files[index].Path || invalidProjectIndexSize(
			files[index].SizeBytes,
			policy.allowEmpty,
		) {
			return model.ProjectIndexView{}, model.ErrCredential
		}
		folded := importing.ASCIICaseFold(normalized)
		if _, duplicate := seen[folded]; duplicate {
			return model.ProjectIndexView{}, model.ErrCredential
		}
		seen[folded] = struct{}{}
		markerFound = markerFound || normalized == policy.marker
		fontFound = fontFound || normalized == policy.font
		files[index].URL = root + escapeProjectPath(normalized)
	}
	if !markerFound || !fontFound {
		return model.ProjectIndexView{}, model.ErrCredential
	}
	contents, err := marshalProjectIndex(title, policy.font, files)
	if err != nil {
		return model.ProjectIndexView{}, err
	}
	digest := sha256.Sum256(contents)
	return model.ProjectIndexView{Contents: contents, SHA256: hex.EncodeToString(digest[:])}, nil
}

func marshalProjectIndex(title, font string, files []runtimeProjectIndexFile) ([]byte, error) {
	var contents []byte
	var err error
	if font != "" {
		contents, err = json.Marshal(onsProjectIndex{Files: files, FontPath: font, SchemaVersion: 1, Title: title})
	} else {
		contents, err = json.Marshal(runtimeProjectIndex{Files: files, SchemaVersion: 1})
	}
	if err != nil {
		return nil, fmt.Errorf("marshal project index: %w", err)
	}
	return contents, nil
}

func escapeProjectPath(logicalName string) string {
	parts := strings.Split(logicalName, "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func invalidProjectIndexSize(size int64, allowEmpty bool) bool {
	return size < 0 || (!allowEmpty && size == 0)
}
