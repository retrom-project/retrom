package storage

import (
	"archive/zip"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/model"
)

func TestPackageRoundTripAndChangedInput(t *testing.T) {
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
	files := make([]model.ContentFile, 0, 3)
	for name, body := range map[string]string{"中文/# file.txt": "payload", "empty": "", "data/inside.xp3": "native container stays intact"} {
		f, writeErr := s.Stage(t.Context(), id, strings.NewReader(body))
		if writeErr != nil {
			t.Fatal(writeErr)
		}
		files = append(files, model.ContentFile{LogicalKey: name, SizeBytes: f.Size, SHA256: f.SHA256, StorageKey: f.Key})
	}
	packed, err := s.BuildPackage(t.Context(), id, files)
	if err != nil {
		t.Fatal(err)
	}
	if packed.Role != "project_package" || len(packed.Members) != len(files) {
		t.Fatalf("package facts: %+v", packed)
	}
	assertStoreMembers(t, s, packed)
	files[0].SHA256 = strings.Repeat("0", 64)
	if _, err = s.BuildPackage(t.Context(), id, files); err == nil {
		t.Fatal("accepted changed source")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = s.BuildPackage(ctx, id, files); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func assertStoreMembers(t *testing.T, s *Store, packed model.GameFile) {
	t.Helper()
	object, err := s.Read(packed.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFile(object)
	archive, err := zip.NewReader(object, packed.SizeBytes)
	if err != nil {
		t.Fatal(err)
	}
	for i, member := range packed.Members {
		if archive.File[i].Method != zip.Store {
			t.Fatal("member compressed")
		}
		reader, openErr := s.OpenContent(packed.StorageKey, member.DataOffset, member.SizeBytes)
		if openErr != nil {
			t.Fatal(openErr)
		}
		bytes, readErr := io.ReadAll(reader)
		closeFile(reader)
		if readErr != nil || int64(len(bytes)) != member.SizeBytes {
			t.Fatalf("member %s: %v", member.LogicalPath, readErr)
		}
	}
}
