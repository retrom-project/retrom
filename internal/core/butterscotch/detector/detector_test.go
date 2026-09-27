package detector

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

type memoryIndex map[string][]byte

func (index memoryIndex) Files() []File {
	files := make([]File, 0, len(index))
	for name, contents := range index {
		files = append(files, File{Path: name, Size: int64(len(contents))})
	}
	return files
}

func (index memoryIndex) Open(name string) (io.ReadCloser, error) {
	contents, exists := index[name]
	if !exists {
		return nil, errors.New("missing file")
	}
	return io.NopCloser(bytes.NewReader(contents)), nil
}

func formFixture() []byte {
	contents := make([]byte, 16)
	copy(contents, "FORM")
	binary.LittleEndian.PutUint32(contents[4:], 8)
	copy(contents[8:], "GEN8")
	return contents
}

func TestDetectAcceptsRootDataWin(t *testing.T) {
	t.Parallel()
	profile, err := Detect(memoryIndex{"data.win": formFixture(), "options.ini": []byte("[options]")})
	if err != nil || profile.MarkerPath != "data.win" ||
		profile.Compatibility != "GAMEMAKER_RUNTIME_TRIAL_REQUIRED" {
		t.Fatalf("profile=%#v error=%v", profile, err)
	}
}

func TestDetectRejectsMissingDuplicateAndInvalidDataWin(t *testing.T) {
	t.Parallel()
	tests := []memoryIndex{
		{"readme.txt": []byte("no marker")},
		{"data.win": []byte("not a GameMaker WAD")},
		{"data.win": formFixture(), "DATA.WIN": formFixture()},
	}
	for _, index := range tests {
		if _, err := Detect(index); !errors.Is(err, ErrProjectInvalid) {
			t.Fatalf("Detect(%#v) error=%v", index, err)
		}
	}
}

func TestSnapshotRoundTripRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	profile := Profile{MarkerPath: "data.win", Compatibility: "GAMEMAKER_RUNTIME_TRIAL_REQUIRED"}
	contents, err := MarshalSnapshot(profile)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseSnapshot(string(contents))
	if err != nil || parsed != profile {
		t.Fatalf("ParseSnapshot()=%#v, %v", parsed, err)
	}
	if _, err := ParseSnapshot(`{"schemaVersion":1,"butterscotch":{"markerPath":"data.win","compatibility":"GAMEMAKER_RUNTIME_TRIAL_REQUIRED"},"extra":true}`); !errors.Is(err, ErrProjectInvalid) {
		t.Fatalf("unknown field error=%v", err)
	}
}
