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
	"unicode/utf8"
)

const maxScriptProbeBytes = 1 << 20

var ErrProjectInvalid = errors.New("ONS_PROJECT_INVALID")

type File struct {
	Path string
	Size int64
}

type Index interface {
	Files() []File
	Open(logicalPath string) (io.ReadCloser, error)
}

type Profile struct {
	MarkerPath     string `json:"markerPath"`
	FontPath       string `json:"fontPath"`
	ScriptEncoding string `json:"scriptEncoding"`
}

type snapshot struct {
	ONS           Profile `json:"ons"`
	SchemaVersion int     `json:"schemaVersion"`
}

var markers = []string{"0.txt", "00.txt", "nscript.dat", "nscr_sec.dat"}

func Markers() []string { return append([]string(nil), markers...) }

func MarshalSnapshot(profile Profile) ([]byte, error) {
	if !validProfile(profile) {
		return nil, ErrProjectInvalid
	}
	contents, err := json.Marshal(snapshot{ONS: profile, SchemaVersion: 1})
	if err != nil {
		return nil, fmt.Errorf("marshal ONS profile: %w", err)
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
	if err := decoder.Decode(&struct{}{}); err != io.EOF || value.SchemaVersion != 1 || !validProfile(value.ONS) {
		return Profile{}, ErrProjectInvalid
	}
	return value.ONS, nil
}

func Detect(index Index) (Profile, error) {
	files := index.Files()
	byPath := make(map[string]File, len(files))
	for _, file := range files {
		key := strings.ToLower(file.Path)
		if file.Path == "" || file.Size < 0 || byPath[key].Path != "" {
			return Profile{}, ErrProjectInvalid
		}
		byPath[key] = file
	}
	markerPath := firstMarker(byPath)
	fontPath := preferredFont(files)
	if markerPath == "" || fontPath == "" {
		return Profile{}, ErrProjectInvalid
	}
	encoding := "gbk"
	if strings.HasSuffix(strings.ToLower(markerPath), ".txt") {
		probe, err := readProbe(index, markerPath)
		if err != nil {
			return Profile{}, fmt.Errorf("%w: script unavailable", ErrProjectInvalid)
		}
		if utf8.ValidString(strings.TrimPrefix(string(probe), "\ufeff")) {
			encoding = "utf8"
		}
	}
	return Profile{MarkerPath: markerPath, FontPath: fontPath, ScriptEncoding: encoding}, nil
}

func firstMarker(files map[string]File) string {
	for _, marker := range markers {
		if file, exists := files[marker]; exists {
			return file.Path
		}
	}
	return ""
}

func preferredFont(files []File) string {
	fonts := make([]string, 0)
	for _, file := range files {
		if strings.EqualFold(path.Ext(file.Path), ".ttf") {
			fonts = append(fonts, file.Path)
		}
	}
	sort.Slice(fonts, func(left, right int) bool {
		leftDefault := strings.EqualFold(fonts[left], "default.ttf")
		rightDefault := strings.EqualFold(fonts[right], "default.ttf")
		if leftDefault != rightDefault {
			return leftDefault
		}
		return strings.ToLower(fonts[left]) < strings.ToLower(fonts[right])
	})
	if len(fonts) == 0 {
		return ""
	}
	return fonts[0]
}

func validProfile(profile Profile) bool {
	markerValid := false
	for _, marker := range markers {
		markerValid = markerValid || strings.EqualFold(profile.MarkerPath, marker)
	}
	return markerValid && validLogicalPath(profile.FontPath) &&
		strings.EqualFold(path.Ext(profile.FontPath), ".ttf") &&
		(profile.ScriptEncoding == "gbk" || profile.ScriptEncoding == "sjis" || profile.ScriptEncoding == "utf8")
}

func validLogicalPath(value string) bool {
	return value != "" && len([]byte(value)) <= 1024 && path.Clean(value) == value &&
		!strings.HasPrefix(value, "/") && !strings.Contains(value, `\`) && !strings.ContainsRune(value, 0)
}

func readProbe(index Index, markerPath string) ([]byte, error) {
	reader, err := index.Open(markerPath)
	if err != nil {
		return nil, fmt.Errorf("open script probe: %w", err)
	}
	defer func() { _ = reader.Close() }()
	probe, err := io.ReadAll(io.LimitReader(reader, maxScriptProbeBytes))
	if err != nil {
		return nil, fmt.Errorf("read script probe: %w", err)
	}
	return probe, nil
}
