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

var ErrProjectInvalid = errors.New("NXENGINE_PROJECT_INVALID")

type File struct {
	Path string
	Size int64
}

type Index interface {
	Files() []File
	Open(logicalPath string) (io.ReadCloser, error)
}

type Profile struct {
	MarkerPath    string `json:"markerPath"`
	Compatibility string `json:"compatibility"`
}

type snapshot struct {
	NXEngine      Profile `json:"nxengine"`
	SchemaVersion int     `json:"schemaVersion"`
}

var markers = []string{"Doukutsu.exe"}

func Markers() []string { return append([]string(nil), markers...) }

func MarshalSnapshot(profile Profile) ([]byte, error) {
	if !validProfile(profile) {
		return nil, ErrProjectInvalid
	}
	contents, err := json.Marshal(snapshot{NXEngine: profile, SchemaVersion: 1})
	if err != nil {
		return nil, fmt.Errorf("marshal NXEngine profile: %w", err)
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
		!validProfile(value.NXEngine) {
		return Profile{}, ErrProjectInvalid
	}
	return value.NXEngine, nil
}

func Detect(index Index) (Profile, error) {
	files := index.Files()
	if len(files) > 4096 {
		return Profile{}, ErrProjectInvalid
	}
	var total int64
	seen := make(map[string]struct{}, len(files))
	markerPath := ""
	markerSize := int64(0)
	for _, file := range files {
		total += file.Size
		if total > 64*1024*1024 {
			return Profile{}, ErrProjectInvalid
		}
		key := strings.ToLower(file.Path)
		if !validPath(file.Path) || file.Size <= 0 || file.Size > 32*1024*1024 {
			return Profile{}, ErrProjectInvalid
		}
		if _, duplicate := seen[key]; duplicate {
			return Profile{}, ErrProjectInvalid
		}
		seen[key] = struct{}{}
		if strings.EqualFold(file.Path, "Doukutsu.exe") {
			markerPath, markerSize = file.Path, file.Size
		}
	}
	if markerPath == "" || markerSize < 128 || markerSize > 16*1024*1024 || !validEXE(index, markerPath) {
		return Profile{}, ErrProjectInvalid
	}
	for _, required := range []string{"data/npc.tbl", "data/stage/start.pxm", "data/stage/start.tsc"} {
		if _, ok := seen[required]; !ok {
			return Profile{}, ErrProjectInvalid
		}
	}
	return Profile{MarkerPath: markerPath, Compatibility: "NXENGINE_RUNTIME_TRIAL_REQUIRED"}, nil
}

func validEXE(index Index, markerPath string) bool {
	reader, err := index.Open(markerPath)
	if err != nil {
		return false
	}
	defer func() { _ = reader.Close() }()
	var header [2]byte
	_, err = io.ReadFull(reader, header[:])
	return err == nil && string(header[:]) == "MZ"
}

func validProfile(profile Profile) bool {
	return strings.EqualFold(profile.MarkerPath, "Doukutsu.exe") &&
		validPath(profile.MarkerPath) && profile.Compatibility == "NXENGINE_RUNTIME_TRIAL_REQUIRED"
}

func validPath(value string) bool {
	return value != "" && len([]byte(value)) <= 1024 && path.Clean(value) == value &&
		value != ".." && !strings.HasPrefix(value, "../") &&
		!strings.Contains(value, ":") && !strings.HasPrefix(value, "/") &&
		!strings.Contains(value, `\`) && !strings.ContainsRune(value, 0)
}
