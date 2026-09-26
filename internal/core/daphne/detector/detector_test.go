package detector

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

type fixture map[string][]byte

func (files fixture) Files() []File {
	result := make([]File, 0, len(files))
	for name, contents := range files {
		result = append(result, File{Path: name, Size: int64(len(contents))})
	}
	return result
}

func (files fixture) Open(name string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(files[name])), nil
}

func TestDetectDaphneProjectAndRejectUnrelatedFramefile(t *testing.T) {
	files := fixture{
		"interstellar.zip": {'P', 'K', 3, 4, 0},
		"interstellar.txt": []byte(".\n\n1\tinterstellar.m2v\n"),
		"interstellar.m2v": {1, 2, 3, 4},
		"interstellar.dat": {1},
		"interstellar.ogg": {1},
	}
	profile, err := Detect(files)
	if err != nil || profile.MarkerPath != "interstellar.zip" || profile.VideoPath != "interstellar.m2v" {
		t.Fatalf("Detect() = %#v, %v", profile, err)
	}
	encoded, err := MarshalSnapshot(profile)
	if err != nil {
		t.Fatal(err)
	}
	if parsed, err := ParseSnapshot(string(encoded)); err != nil || parsed != profile {
		t.Fatalf("ParseSnapshot() = %#v, %v", parsed, err)
	}
	files["interstellar.txt"] = []byte(".\n1 unrelated.m2v\n")
	if _, err := Detect(files); !errors.Is(err, ErrProjectInvalid) {
		t.Fatalf("invalid framefile error = %v", err)
	}
}
