package detector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

var ErrProjectInvalid = errors.New("TYRANOSCRIPT_PROJECT_INVALID")

type File struct {
	Path string
	Size int64
}

type Index interface {
	Files() []File
}

type Profile struct {
	EntryPath     string `json:"entryPath"`
	Compatibility string `json:"compatibility"`
}

type snapshot struct {
	SchemaVersion int     `json:"schemaVersion"`
	TyranoScript  Profile `json:"tyranoScript"`
}

var markers = []string{
	"index.html",
	"data/scenario/first.ks",
	"data/system/Config.tjs",
	"tyrano/plugins/kag/kag.js",
	"tyrano/tyrano.js",
}

func Markers() []string { return append([]string(nil), markers...) }

func Detect(index Index) (Profile, error) {
	seen := make(map[string]struct{}, len(index.Files()))
	for _, file := range index.Files() {
		key := strings.ToLower(file.Path)
		if !validPath(file.Path) || file.Size < 0 {
			return Profile{}, ErrProjectInvalid
		}
		if _, duplicate := seen[key]; duplicate {
			return Profile{}, ErrProjectInvalid
		}
		seen[key] = struct{}{}
	}
	for _, marker := range markers {
		if _, exists := seen[strings.ToLower(marker)]; !exists {
			return Profile{}, ErrProjectInvalid
		}
	}
	return Profile{EntryPath: "index.html", Compatibility: "TYRANOSCRIPT_RUNTIME_TRIAL_REQUIRED"}, nil
}

func MarshalSnapshot(profile Profile) ([]byte, error) {
	if !validProfile(profile) {
		return nil, ErrProjectInvalid
	}
	contents, err := json.Marshal(snapshot{SchemaVersion: 1, TyranoScript: profile})
	if err != nil {
		return nil, fmt.Errorf("marshal TyranoScript profile: %w", err)
	}
	return contents, nil
}

func ParseSnapshot(contents string) (Profile, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(contents))
	decoder.DisallowUnknownFields()
	var value snapshot
	if err := decoder.Decode(&value); err != nil {
		return Profile{}, ErrProjectInvalid
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF || value.SchemaVersion != 1 ||
		!validProfile(value.TyranoScript) {
		return Profile{}, ErrProjectInvalid
	}
	return value.TyranoScript, nil
}

func validProfile(profile Profile) bool {
	return profile.EntryPath == "index.html" &&
		profile.Compatibility == "TYRANOSCRIPT_RUNTIME_TRIAL_REQUIRED"
}

func validPath(value string) bool {
	return value != "" && len([]byte(value)) <= 1024 && path.Clean(value) == value &&
		!strings.HasPrefix(value, "/") && !strings.Contains(value, `\`) && !strings.ContainsRune(value, 0)
}
