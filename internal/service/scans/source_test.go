package scans

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/model"
	"retrom/internal/storage"
)

func TestInspectAbsoluteDirectoryKeepsSourceKeysAndReportsAbsolutePaths(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	child := filepath.Join(directory, "nes")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	metadata := []byte("collection: NES\n\ngame: Test\nfile: game.nes\n")
	if err := os.WriteFile(filepath.Join(child, "metadata.pegasus.txt"), metadata, 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Service{}
	input := model.SourceInput{Path: directory, Format: "pegasus"}
	admin := model.Principal{User: model.User{Role: "admin", Status: "active"}}
	items, err := s.Inspect(t.Context(), admin, input)
	if err != nil || len(items) != 1 || items[0].Path != child || items[0].Key != "nes:0" || items[0].GameCount != 1 {
		t.Fatalf("collections=%+v error=%v", items, err)
	}
	if _, err = s.Inspect(t.Context(), model.Principal{User: model.User{Role: "user", Status: "active"}}, input); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("nonadmin inspect error=%v", err)
	}
}

func TestAbsoluteReplacementSourceCopiesFileAndLinkedProject(t *testing.T) {
	t.Parallel()
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	s := &Service{Storage: store}
	project := t.TempDir()
	payload := []byte("source copy remains independent of managed content")
	source := filepath.Join(project, "game.nes")
	if err = os.WriteFile(source, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "selected-project")
	if err = os.Symlink(project, link); err != nil {
		t.Fatal(err)
	}
	for _, selected := range []string{source, link} {
		root, name, openErr := s.Sources.OpenContent(selected)
		if openErr != nil {
			t.Fatal(openErr)
		}
		files, _, copyErr := s.contentFile(t.Context(), root, "11111111-1111-4111-8111-111111111111", name, model.Directory{})
		closeErr := root.Close()
		if copyErr != nil || closeErr != nil || len(files) != 1 || files[0].LogicalKey != "game.nes" {
			t.Fatalf("selected=%s files=%+v copy=%v close=%v", selected, files, copyErr, closeErr)
		}
		assertStoredBytes(t, store, files[0].StorageKey, payload)
	}
	actual, err := os.ReadFile(source)
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatalf("source changed: %q error=%v", actual, err)
	}
}

func assertStoredBytes(t *testing.T, store *storage.Store, key string, payload []byte) {
	t.Helper()
	file, err := store.Read(key)
	if err != nil {
		t.Fatal(err)
	}
	actual, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(actual, payload) {
		t.Fatalf("managed bytes=%q read=%v close=%v", actual, readErr, closeErr)
	}
}
