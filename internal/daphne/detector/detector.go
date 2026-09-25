package detector

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

var ErrProjectInvalid = errors.New("DAPHNE_PROJECT_INVALID")

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
	FramefilePath string `json:"framefilePath"`
	VideoPath     string `json:"videoPath"`
}

type snapshot struct {
	SchemaVersion int     `json:"schemaVersion"`
	Daphne        Profile `json:"daphne"`
}

func Detect(index Index) (Profile, error) {
	files := index.Files()
	if len(files) < 3 || len(files) > 16 {
		return Profile{}, ErrProjectInvalid
	}
	seen := make(map[string]File, len(files))
	var rom, video File
	var total int64
	for _, file := range files {
		name := strings.ToLower(file.Path)
		if !validName(file.Path) || file.Size < 1 || file.Size > 2_147_483_647 {
			return Profile{}, ErrProjectInvalid
		}
		if _, duplicate := seen[name]; duplicate {
			return Profile{}, ErrProjectInvalid
		}
		seen[name] = file
		total += file.Size
		if total > 2_147_483_647 {
			return Profile{}, ErrProjectInvalid
		}
		if strings.HasSuffix(name, ".zip") {
			if rom.Path != "" || file.Size > 64<<20 {
				return Profile{}, ErrProjectInvalid
			}
			rom = file
		} else if strings.HasSuffix(name, ".m2v") {
			if video.Path != "" {
				return Profile{}, ErrProjectInvalid
			}
			video = file
		} else if file.Size > 64<<20 {
			return Profile{}, ErrProjectInvalid
		}
	}
	if rom.Path == "" || video.Path == "" {
		return Profile{}, ErrProjectInvalid
	}
	frame, ok := seen[strings.ToLower(rom.Path[:len(rom.Path)-4]+".txt")]
	if !ok || frame.Size > 64<<10 || !zipHeader(index, rom.Path) || !framefileReferences(index, frame.Path, video.Path) {
		return Profile{}, ErrProjectInvalid
	}
	return Profile{MarkerPath: rom.Path, FramefilePath: frame.Path, VideoPath: video.Path}, nil
}

func validName(name string) bool {
	if name == "" || len(name) > 128 || strings.Contains(name, "..") || strings.ContainsAny(name, "/\\:") {
		return false
	}
	for _, char := range name {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9' || char == '.' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	for _, suffix := range []string{".zip", ".txt", ".m2v", ".dat", ".ogg"} {
		if strings.HasSuffix(strings.ToLower(name), suffix) {
			return true
		}
	}
	return false
}

func zipHeader(index Index, name string) bool {
	reader, err := index.Open(name)
	if err != nil {
		return false
	}
	defer func() { _ = reader.Close() }()
	var header [4]byte
	_, err = io.ReadFull(reader, header[:])
	return err == nil && bytes.Equal(header[:], []byte{'P', 'K', 3, 4})
}

func framefileReferences(index Index, name, video string) bool {
	reader, err := index.Open(name)
	if err != nil {
		return false
	}
	defer func() { _ = reader.Close() }()
	contents, err := io.ReadAll(io.LimitReader(reader, 64<<10+1))
	if err != nil || len(contents) > 64<<10 {
		return false
	}
	lines := strings.Split(strings.ReplaceAll(string(contents), "\r", ""), "\n")
	first := ""
	found := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if first == "" {
			first = line
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.EqualFold(fields[1], video) {
			found = true
		}
	}
	return first == "." && found
}

func MarshalSnapshot(profile Profile) ([]byte, error) {
	if !validProfile(profile) {
		return nil, ErrProjectInvalid
	}
	return json.Marshal(snapshot{SchemaVersion: 1, Daphne: profile})
}

func ParseSnapshot(raw string) (Profile, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value snapshot
	if decoder.Decode(&value) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		value.SchemaVersion != 1 || !validProfile(value.Daphne) {
		return Profile{}, ErrProjectInvalid
	}
	return value.Daphne, nil
}

func validProfile(profile Profile) bool {
	return validName(profile.MarkerPath) && strings.HasSuffix(strings.ToLower(profile.MarkerPath), ".zip") &&
		validName(profile.FramefilePath) && strings.EqualFold(profile.FramefilePath,
		profile.MarkerPath[:len(profile.MarkerPath)-4]+".txt") &&
		validName(profile.VideoPath) && strings.HasSuffix(strings.ToLower(profile.VideoPath), ".m2v")
}
