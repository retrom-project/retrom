//go:build storeziplarge

package storage

import (
	"io"
	"testing"

	"retrom/internal/model"

	"github.com/google/uuid"
)

type fillReader byte

func (r fillReader) Read(buffer []byte) (int, error) {
	for i := range buffer {
		buffer[i] = byte(r)
	}
	return len(buffer), nil
}

// Run explicitly: go test -tags storeziplarge ./internal/storage -run TestZIP64PackageBeyondEightGiB -v.
func TestZIP64PackageBeyondEightGiB(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := s.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	id := uuid.NewString()
	files := make([]model.ContentFile, 0, 2)
	for i, size := range []int64{MaximumFileSize, 1024 * 1024 * 1024} {
		file, writeErr := s.Stage(t.Context(), id, io.LimitReader(fillReader('A'+byte(i)), size))
		if writeErr != nil {
			t.Fatal(writeErr)
		}
		name := []string{"a.bin", "b.bin"}[i]
		files = append(files, model.ContentFile{LogicalKey: name, StorageKey: file.Key, SizeBytes: file.Size, SHA256: file.SHA256})
	}
	file, err := s.BuildPackage(t.Context(), id, files)
	if err != nil {
		t.Fatal(err)
	}
	if file.SizeBytes <= MaximumFileSize || len(file.Members) != 2 || file.Members[1].DataOffset <= 1<<32 {
		t.Fatal("ZIP64 facts lost")
	}
	for i, member := range file.Members {
		reader, readErr := s.OpenContent(file.StorageKey, member.DataOffset, member.SizeBytes)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, readErr = reader.Seek(-1, io.SeekEnd); readErr != nil {
			t.Fatal(readErr)
		}
		body, readErr := io.ReadAll(reader)
		closeFile(reader)
		if readErr != nil || len(body) != 1 || body[0] != 'A'+byte(i) {
			t.Fatal("ZIP64 tail crossed member")
		}
	}
	t.Logf("physical=%d first-member=%d second-offset=%d", file.SizeBytes, file.Members[0].SizeBytes, file.Members[1].DataOffset)
}
