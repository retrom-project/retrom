package materializer

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"testing"

	"retrom/internal/testassert"
)

func TestWriteMKXPZIsDeterministicSortedStoredArchive(t *testing.T) {
	files := []SourceFile{
		memorySource("Data/Scripts.rxdata", []byte("scripts")),
		memorySource("Game.ini", []byte("[Game]\nScripts=Data/Scripts.rxdata\n")),
	}
	var first, second bytes.Buffer
	result, err := WriteMKXPZ(&first, files)
	reversed := []SourceFile{files[1], files[0]}
	resultAgain, errAgain := WriteMKXPZ(&second, reversed)
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil || errAgain != nil },
		func() bool { return !bytes.Equal(first.Bytes(), second.Bytes()) },
		func() bool { return result.SHA256 != resultAgain.SHA256 },
		func() bool {
			return result.UncompressedSize != int64(len("scripts")+len("[Game]\nScripts=Data/Scripts.rxdata\n"))
		},
	), "result=%#v/%#v error=%v/%v", result, resultAgain, err, errAgain)

	reader, err := zip.NewReader(bytes.NewReader(first.Bytes()), int64(first.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 2 || reader.File[0].Name != "Data/Scripts.rxdata" ||
		reader.File[1].Name != "Game.ini" {
		t.Fatalf("archive order=%v", archiveNames(reader.File))
	}
	for _, file := range reader.File {
		if file.Method != zip.Store || len(file.Extra) != 0 || file.Mode().Perm() != 0o644 {
			t.Fatalf("non-deterministic header: %#v", file.FileHeader)
		}
	}
}

func TestWriteMKXPZRejectsUnsafeCollisionAndSizeDrift(t *testing.T) {
	tests := map[string][]SourceFile{
		"traversal": {memorySource("../Game.ini", nil)},
		"nfkc collision": {
			memorySource("Data/Ｋ.txt", nil),
			memorySource("data/k.TXT", nil),
		},
		"short read": {{Path: "Game.ini", Size: 2, Open: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader([]byte{1})), nil
		}}},
	}
	for name, files := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := WriteMKXPZ(io.Discard, files); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error=%v, want %v", err, ErrInvalid)
			}
		})
	}
}

func TestStoreZIPHeaderHasFixedWireFields(t *testing.T) {
	header := StoreZIPHeader("Game.ini")
	if !header.Modified.IsZero() || len(header.Extra) != 0 ||
		header.Method != zip.Store || header.Mode().Perm() != 0o644 {
		t.Fatalf("header=%#v", header)
	}
}

func memorySource(path string, data []byte) SourceFile {
	return SourceFile{Path: path, Size: int64(len(data)), Open: func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}}
}

func archiveNames(files []*zip.File) []string {
	result := make([]string, 0, len(files))
	for _, file := range files {
		result = append(result, file.Name)
	}
	return result
}
