package detector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

var ErrProjectInvalid = errors.New("KIRIKIRI_PROJECT_INVALID")

type File struct {
	Path string
	Size int64
}

type Index interface {
	Files() []File
}

type Profile struct {
	MarkerPath     string  `json:"markerPath"`
	StartupXP3Path *string `json:"startupXp3Path"`
	Compatibility  string  `json:"compatibility"`
}

type snapshot struct {
	KiriKiri      Profile `json:"kirikiri"`
	SchemaVersion int     `json:"schemaVersion"`
}

var markers = []string{"startup.tjs", "data.xp3"}

func Markers() []string { return append([]string(nil), markers...) }

func MarshalSnapshot(profile Profile) ([]byte, error) {
	if !validProfile(profile) {
		return nil, ErrProjectInvalid
	}
	contents, err := json.Marshal(snapshot{KiriKiri: profile, SchemaVersion: 1})
	if err != nil {
		return nil, fmt.Errorf("marshal KiriKiri profile: %w", err)
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
	if err := decoder.Decode(&struct{}{}); err != io.EOF || value.SchemaVersion != 1 || !validProfile(value.KiriKiri) {
		return Profile{}, ErrProjectInvalid
	}
	return value.KiriKiri, nil
}

func Detect(index Index) (Profile, error) {
	files := index.Files()
	byPath := make(map[string]File, len(files))
	xp3Paths := make([]string, 0)
	for _, file := range files {
		key := strings.ToLower(file.Path)
		if !validPath(file.Path) || file.Size < 0 || byPath[key].Path != "" {
			return Profile{}, ErrProjectInvalid
		}
		byPath[key] = file
		if strings.EqualFold(path.Ext(file.Path), ".xp3") {
			xp3Paths = append(xp3Paths, file.Path)
		}
	}
	markerPath := ""
	if file := byPath["startup.tjs"]; file.Path != "" {
		markerPath = file.Path
	} else if file := byPath["data.xp3"]; file.Path != "" {
		markerPath = file.Path
	}
	if markerPath == "" {
		return Profile{}, ErrProjectInvalid
	}
	sort.Slice(xp3Paths, func(left, right int) bool {
		return strings.ToLower(xp3Paths[left]) < strings.ToLower(xp3Paths[right])
	})
	selectedXP3, err := selectXP3(xp3Paths)
	if err != nil {
		return Profile{}, err
	}
	var startupXP3 *string
	if selectedXP3 != "" {
		startupXP3 = &selectedXP3
	}
	return Profile{
		MarkerPath: markerPath, StartupXP3Path: startupXP3, Compatibility: "KAG_RUNTIME_TRIAL_REQUIRED",
	}, nil
}

func selectXP3(paths []string) (string, error) {
	if len(paths) == 0 {
		return "", nil
	}
	for _, value := range paths {
		if strings.EqualFold(value, "data.xp3") {
			return value, nil
		}
	}
	if len(paths) != 1 {
		return "", fmt.Errorf("%w: ambiguous XP3 entry", ErrProjectInvalid)
	}
	return paths[0], nil
}

func validProfile(profile Profile) bool {
	if !validPath(profile.MarkerPath) || profile.Compatibility != "KAG_RUNTIME_TRIAL_REQUIRED" {
		return false
	}
	if !strings.EqualFold(profile.MarkerPath, "startup.tjs") && !strings.EqualFold(profile.MarkerPath, "data.xp3") {
		return false
	}
	return profile.StartupXP3Path == nil || validPath(*profile.StartupXP3Path) &&
		strings.EqualFold(path.Ext(*profile.StartupXP3Path), ".xp3")
}

func validPath(value string) bool {
	return value != "" && len([]byte(value)) <= 1024 && path.Clean(value) == value &&
		!strings.HasPrefix(value, "/") && !strings.Contains(value, `\`) && !strings.ContainsRune(value, 0)
}
