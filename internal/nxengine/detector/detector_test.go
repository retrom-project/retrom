package detector

import (
	"bytes"
	"io"
	"testing"
)

type testIndex map[string][]byte

func (index testIndex) Files() []File {
	files := make([]File, 0, len(index))
	for name, data := range index {
		files = append(files, File{Path: name, Size: int64(len(data))})
	}
	return files
}

func (index testIndex) Open(name string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(index[name])), nil
}

func TestRequireCompleteGameAndRejectAmbiguousPaths(t *testing.T) {
	index := testIndex{"Doukutsu.exe": append([]byte("MZ"), make([]byte, 126)...), "data/npc.tbl": {1}, "data/Stage/Start.pxm": {1}, "data/Stage/Start.tsc": {1}}
	profile, err := Detect(index)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := MarshalSnapshot(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseSnapshot(string(snapshot)); err != nil {
		t.Fatal(err)
	}
	delete(index, "data/npc.tbl")
	if _, err := Detect(index); err == nil {
		t.Fatal("missing assets accepted")
	}
	index["data/npc.tbl"] = []byte{1}
	index["doukutsu.exe"] = index["Doukutsu.exe"]
	if _, err := Detect(index); err == nil {
		t.Fatal("ambiguous executable accepted")
	}
}
