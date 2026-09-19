package detector

import (
	"errors"
	"testing"
)

func TestSelectAndDetectPreferDefaultFontAndUTF8Script(t *testing.T) {
	t.Parallel()
	selection, err := Select([]File{
		{Path: "0.txt", Size: 17},
		{Path: "fonts/other.ttf", Size: 1},
		{Path: "default.ttf", Size: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := Detect(selection, []byte("*define\n;mode800\n"))
	if err != nil || profile.MarkerPath != "0.txt" || profile.FontPath != "default.ttf" ||
		profile.ScriptEncoding != "utf8" {
		t.Fatalf("profile=%#v error=%v", profile, err)
	}
	if selection.ProbePath() != "0.txt" {
		t.Fatalf("ProbePath() = %q", selection.ProbePath())
	}
}

func TestDetectAcceptsEncryptedScriptAndDefaultsToGBK(t *testing.T) {
	t.Parallel()
	selection, err := Select([]File{{Path: "nscript.dat", Size: 2}, {Path: "font.ttf", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := Detect(selection, nil)
	if err != nil || profile.ScriptEncoding != "gbk" || selection.ProbePath() != "" {
		t.Fatalf("profile=%#v probe=%q error=%v", profile, selection.ProbePath(), err)
	}
}

func TestSnapshotRoundTripRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	profile := Profile{MarkerPath: "0.txt", FontPath: "font/default.ttf", ScriptEncoding: "utf8"}
	contents, err := MarshalSnapshot(profile)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseSnapshot(string(contents))
	if err != nil || parsed != profile {
		t.Fatalf("ParseSnapshot() = %#v, %v", parsed, err)
	}
	if _, err := ParseSnapshot(`{"schemaVersion":1,"ons":{"markerPath":"0.txt","fontPath":"default.ttf","scriptEncoding":"utf8"},"extra":true}`); !errors.Is(err, ErrProjectInvalid) {
		t.Fatalf("unknown field error = %v", err)
	}
}

func TestSelectRequiresScriptAndFont(t *testing.T) {
	t.Parallel()
	tests := [][]File{
		{{Path: "0.txt", Size: 7}},
		{{Path: "default.ttf", Size: 1}},
		{{Path: "0.txt", Size: 1}, {Path: "0.TXT", Size: 1}, {Path: "font.ttf", Size: 1}},
	}
	for _, files := range tests {
		if _, err := Select(files); !errors.Is(err, ErrProjectInvalid) {
			t.Fatalf("Select(%#v) error=%v", files, err)
		}
	}
}

func TestDetectRejectsEmptySelection(t *testing.T) {
	t.Parallel()
	if _, err := Detect(Selection{}, nil); !errors.Is(err, ErrProjectInvalid) {
		t.Fatalf("Detect() error=%v", err)
	}
}
