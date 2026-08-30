package detector

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

var ErrProjectInvalid = errors.New("BUTTERSCOTCH_PROJECT_INVALID")

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
	Butterscotch  Profile `json:"butterscotch"`
	SchemaVersion int     `json:"schemaVersion"`
}

var markers = []string{"data.win"}

func Markers() []string { return append([]string(nil), markers...) }

func MarshalSnapshot(profile Profile) ([]byte, error) {
	if !validProfile(profile) {
		return nil, ErrProjectInvalid
	}
	contents, err := json.Marshal(snapshot{Butterscotch: profile, SchemaVersion: 1})
	if err != nil {
		return nil, fmt.Errorf("marshal Butterscotch profile: %w", err)
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
		!validProfile(value.Butterscotch) {
		return Profile{}, ErrProjectInvalid
	}
	return value.Butterscotch, nil
}

func Detect(index Index) (Profile, error) {
	files := index.Files()
	seen := make(map[string]struct{}, len(files))
	markerPath := ""
	markerSize := int64(0)
	for _, file := range files {
		key := strings.ToLower(file.Path)
		if !validPath(file.Path) || file.Size < 0 {
			return Profile{}, ErrProjectInvalid
		}
		if _, duplicate := seen[key]; duplicate {
			return Profile{}, ErrProjectInvalid
		}
		seen[key] = struct{}{}
		if strings.EqualFold(file.Path, "data.win") {
			markerPath, markerSize = file.Path, file.Size
		}
	}
	if markerPath == "" || markerSize < 16 || !validFORM(index, markerPath, markerSize) {
		return Profile{}, ErrProjectInvalid
	}
	return Profile{MarkerPath: markerPath, Compatibility: "GAMEMAKER_RUNTIME_TRIAL_REQUIRED"}, nil
}

func validFORM(index Index, markerPath string, markerSize int64) bool {
	reader, err := index.Open(markerPath)
	if err != nil {
		return false
	}
	defer func() { _ = reader.Close() }()
	header := make([]byte, 8)
	if _, err := io.ReadFull(reader, header); err != nil || string(header[:4]) != "FORM" {
		return false
	}
	declared := int64(binary.LittleEndian.Uint32(header[4:]))
	return declared >= 8 && declared <= markerSize-8
}

func validProfile(profile Profile) bool {
	return strings.EqualFold(profile.MarkerPath, "data.win") &&
		validPath(profile.MarkerPath) && profile.Compatibility == "GAMEMAKER_RUNTIME_TRIAL_REQUIRED"
}

func validPath(value string) bool {
	return value != "" && len([]byte(value)) <= 1024 && path.Clean(value) == value &&
		!strings.HasPrefix(value, "/") && !strings.Contains(value, `\`) && !strings.ContainsRune(value, 0)
}
