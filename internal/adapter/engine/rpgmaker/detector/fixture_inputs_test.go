package detector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	policy "retrom/internal/capability/engine/rpgmaker/detector"
)

type capturedInput struct {
	Name, Core, Directory, Target, Mode string
	Sizes                               map[string]int64
	Contents                            map[string][]byte
}

type capturedBound struct {
	Name, Path              string
	Limit, Declared, Actual int64
	Code                    policy.Code
}

func fixtureValue[T any](t *testing.T, name string) T {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var result T
	if err := json.Unmarshal(contents, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func publicFixtureRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate public fixtures")
	}
	return filepath.Clean(filepath.Join(
		filepath.Dir(filename), "..", "..", "..", "..", "..", "testdata", "public-roms", "rpgmaker-smoke",
	))
}

func capturedSource(t *testing.T, input capturedInput) fileIndex {
	t.Helper()
	if input.Directory != "" {
		return readPublicFixtureIndex(t, filepath.Join(publicFixtureRoot(t), filepath.FromSlash(input.Directory)))
	}
	return memoryFixture(input.Contents)
}

func namedInput(t *testing.T, name string) capturedInput {
	t.Helper()
	for _, input := range fixtureValue[[]capturedInput](t, "fixture-inputs.json") {
		if input.Name == name {
			return input
		}
	}
	t.Fatalf("missing captured input %q", name)
	return capturedInput{}
}

type memoryFixture map[string][]byte

func (source memoryFixture) Files() []policy.File {
	files := make([]policy.File, 0, len(source))
	for name, contents := range source {
		files = append(files, policy.File{Path: name, Size: int64(len(contents))})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func (source memoryFixture) Open(name string) (io.ReadCloser, error) {
	contents, exists := source[name]
	if !exists {
		return nil, fmt.Errorf("missing captured file %q", name)
	}
	return io.NopCloser(bytes.NewReader(contents)), nil
}

type repeatedIndex struct {
	name             string
	declared, actual int64
}

func (index repeatedIndex) Files() []policy.File {
	return []policy.File{{Path: index.name, Size: index.declared}}
}

func (index repeatedIndex) Open(string) (io.ReadCloser, error) {
	return &repeatedReader{remaining: index.actual}, nil
}

type repeatedReader struct{ remaining int64 }

func (reader *repeatedReader) Read(buffer []byte) (int, error) {
	if reader.remaining == 0 {
		return 0, io.EOF
	}
	length := min(int64(len(buffer)), reader.remaining)
	clear(buffer[:length])
	reader.remaining -= length
	return int(length), nil
}

func (*repeatedReader) Close() error { return nil }
