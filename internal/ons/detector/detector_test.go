package detector

import (
	"bytes"
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
	return io.NopCloser(bytes.NewReader(index[name])), nil
}

func TestDetectPrefersDefaultFontAndUTF8Script(t *testing.T) {
	t.Parallel()
	profile, err := Detect(memoryIndex{
		"0.txt": []byte("*define\n;mode800\n"), "fonts/other.ttf": {1}, "default.ttf": {2},
	})
	if err != nil || profile.MarkerPath != "0.txt" || profile.FontPath != "default.ttf" ||
		profile.ScriptEncoding != "utf8" {
		t.Fatalf("profile=%#v error=%v", profile, err)
	}
}

func TestDetectAcceptsEncryptedScriptAndDefaultsToGBK(t *testing.T) {
	t.Parallel()
	profile, err := Detect(memoryIndex{"nscript.dat": {0x81, 0xff}, "font.ttf": {1}})
	if err != nil || profile.ScriptEncoding != "gbk" {
		t.Fatalf("profile=%#v error=%v", profile, err)
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

func TestDetectRequiresScriptAndFont(t *testing.T) {
	t.Parallel()
	for _, index := range []memoryIndex{{"0.txt": []byte("*define")}, {"default.ttf": {1}}} {
		if _, err := Detect(index); !errors.Is(err, ErrProjectInvalid) {
			t.Fatalf("Detect(%#v) error=%v", index, err)
		}
	}
}
