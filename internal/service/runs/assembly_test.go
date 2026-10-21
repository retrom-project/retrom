package runs

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"testing"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
	"retrom/internal/storage"

	"github.com/google/uuid"
)

func TestNativeZIPReuseAndMappedRunTemporary(t *testing.T) {
	s, selected := resourcePackage(t)
	service := &Service{Storage: s}
	run := &Context{Run: model.Run{ID: uuid.NewString()}}
	plan := runtimeclient.Resource{Kind: "ASSEMBLED_ZIP"}
	reused, err := service.assembled(t.Context(), run, selected, plan)
	if err != nil {
		t.Fatal(err)
	}
	if reused.Key != selected[0].Key || reused.Offset != 0 || len(run.TemporaryKeys) != 0 ||
		reused.SHA256 != selected[0].PackageSHA256 || reused.SizeBytes != selected[0].PackageSize {
		t.Fatal("unchanged complete package was copied or exposed as a member")
	}
	plan.Paths = map[string]string{"one.txt": "mapped/one.txt"}
	assembled, err := service.assembled(t.Context(), run, selected[:1], plan)
	if err != nil {
		t.Fatal(err)
	}
	if path.Dir(assembled.Key) != "temporary/"+run.Run.ID || len(run.TemporaryKeys) != 1 {
		t.Fatalf("assembly is not owned by Run: %+v", assembled)
	}
	assertMappedZIP(t, s, assembled)
	service.releaseTemporary(run.TemporaryKeys)
	for _, key := range []string{assembled.Key, path.Dir(assembled.Key)} {
		file, openErr := s.Read(key)
		if openErr == nil {
			if closeErr := file.Close(); closeErr != nil {
				t.Error(closeErr)
			}
		}
		if !errors.Is(openErr, os.ErrNotExist) {
			t.Fatalf("temporary resource survived close: %s: %v", key, openErr)
		}
	}
}

func resourcePackage(t *testing.T) (*storage.Store, []Blob) {
	t.Helper()
	s, err := storage.Open(t.TempDir())
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
	for _, name := range []string{"one.txt", "two.txt"} {
		file, writeErr := s.Stage(t.Context(), id, strings.NewReader(name))
		if writeErr != nil {
			t.Fatal(writeErr)
		}
		files = append(files, model.ContentFile{
			LogicalKey: name, StorageKey: file.Key, SHA256: file.SHA256, SizeBytes: file.Size,
		})
	}
	packed, err := s.BuildPackage(t.Context(), id, files)
	if err != nil {
		t.Fatal(err)
	}
	selected := make([]Blob, 0, len(packed.Members))
	for _, member := range packed.Members {
		selected = append(selected, blob(model.ContentFile{
			LogicalKey: member.LogicalPath, StorageKey: packed.StorageKey, Offset: member.DataOffset,
			SHA256: member.SHA256, SizeBytes: member.SizeBytes, Package: &packed,
		}))
	}
	return s, selected
}

func assertMappedZIP(t *testing.T, s *storage.Store, assembled Blob) {
	t.Helper()
	file, err := s.Read(assembled.Key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	archive, err := zip.NewReader(file, assembled.SizeBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) != 1 || archive.File[0].Name != "mapped/one.txt" || archive.File[0].Method != zip.Store {
		t.Fatal("subset or mapping was lost")
	}
	reader, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	bytes, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || string(bytes) != "one.txt" {
		t.Fatalf("assembled member differs: %q, %v, %v", bytes, readErr, closeErr)
	}
}
