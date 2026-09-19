package detector

import (
	"encoding/binary"
	"errors"
	"testing"
)

func formHeader(declared uint32) [8]byte {
	var header [8]byte
	copy(header[:], "FORM")
	binary.LittleEndian.PutUint32(header[4:], declared)
	return header
}

func TestSelectAndDetectAcceptRootDataWin(t *testing.T) {
	t.Parallel()
	selection, err := Select([]File{{Path: "data.win", Size: 16}, {Path: "options.ini", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := Detect(selection, formHeader(8))
	if err != nil || profile.MarkerPath != "data.win" ||
		profile.Compatibility != "GAMEMAKER_RUNTIME_TRIAL_REQUIRED" {
		t.Fatalf("profile=%#v error=%v", profile, err)
	}
	if selection.ProbePath() != "data.win" {
		t.Fatalf("ProbePath() = %q", selection.ProbePath())
	}
}

func TestSelectRejectsMissingDuplicateAndUndersizedDataWin(t *testing.T) {
	t.Parallel()
	tests := [][]File{
		{{Path: "readme.txt", Size: 1}},
		{{Path: "data.win", Size: 15}},
		{{Path: "data.win", Size: 16}, {Path: "DATA.WIN", Size: 16}},
	}
	for _, files := range tests {
		if _, err := Select(files); !errors.Is(err, ErrProjectInvalid) {
			t.Fatalf("Select(%#v) error=%v", files, err)
		}
	}
}

func TestDetectEnforcesFORMBounds(t *testing.T) {
	t.Parallel()
	selection, err := Select([]File{{Path: "data.win", Size: 20}})
	if err != nil {
		t.Fatal(err)
	}
	for _, header := range [][8]byte{formHeader(7), formHeader(13), {}} {
		if _, err := Detect(selection, header); !errors.Is(err, ErrProjectInvalid) {
			t.Fatalf("Detect(%v) error=%v", header, err)
		}
	}
	if _, err := Detect(selection, formHeader(12)); err != nil {
		t.Fatalf("upper bound rejected: %v", err)
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
