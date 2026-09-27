package filestore

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
)

func TestPublishDirectoryResumesAfterRenameAndKeepsOtherGamesIndependent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	original, err := store.Put(bytes.NewBufferString("independent game content"))
	if err != nil {
		t.Fatal(err)
	}
	first, second := uuid.NewString(), uuid.NewString()
	game, otherGame := uuid.NewString(), uuid.NewString()
	one, err := store.CopyTo(t.Context(), original.Record,
		ItemDirectory(first)+"/payload/content", "project/data/game.bin")
	if err != nil {
		t.Fatal(err)
	}
	two, err := store.CopyTo(t.Context(), original.Record,
		ItemDirectory(second)+"/payload/content", "project/data/game.bin")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := store.PublishDirectory(first, game); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.PublishDirectory(second, otherGame); err != nil {
		t.Fatal(err)
	}
	published, err := PublishedRecord(one.Record, first, game)
	if err != nil {
		t.Fatal(err)
	}
	other, err := PublishedRecord(two.Record, second, otherGame)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(one.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workspace remains: %v", err)
	}
	assertGamesIndependent(t, store, published, other, game)
}

func assertGamesIndependent(t *testing.T, store *Store, published, other, game string) {
	t.Helper()
	firstInfo, err := os.Stat(store.Path(published))
	if err != nil {
		t.Fatal(err)
	}
	secondInfo, err := os.Stat(store.Path(other))
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(firstInfo, secondInfo) {
		t.Fatal("games share an inode")
	}
	if err := store.RemovePath(t.Context(), GameDirectory(game)); err != nil {
		t.Fatal(err)
	}
	if err := store.RemovePath(t.Context(), GameDirectory(game)); err != nil {
		t.Fatal(err)
	}
	file, err := store.OpenRecord(other)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close game file", file.Close()) }()
	content, err := io.ReadAll(file)
	if err != nil || string(content) != "independent game content" {
		t.Fatalf("other game changed: %q %v", content, err)
	}
}

func TestWorkspaceRejectsEscapesSymlinksAndChangedSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.Put(bytes.NewBufferString("original"))
	if err != nil {
		t.Fatal(err)
	}
	item := ItemDirectory(uuid.NewString())
	for _, name := range []string{"../escape", "/absolute", "folder/../../escape", "folder\\escape"} {
		if _, err := store.CopyTo(t.Context(), source.Record, item+"/payload", name); err == nil {
			t.Fatalf("accepted path %q", name)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, item), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, item, "payload")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CopyTo(t.Context(), source.Record, item+"/payload", "game.bin"); err == nil {
		t.Fatal("followed workspace symlink")
	}
	if err := os.WriteFile(source.Path, []byte("modified"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := ItemDirectory(uuid.NewString()) + "/payload/content"
	if _, err := store.CopyTo(t.Context(), source.Record, destination, "game.bin"); err == nil {
		t.Fatal("accepted modified input")
	}
	if _, err := os.Stat(filepath.Join(root, destination, "game.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("kept corrupt output: %v", err)
	}
	for _, relative := range []string{".", "files", "staging/items", "retrom.db", "secrets/launch-capability.key"} {
		if err := store.RemovePath(t.Context(), relative); !errors.Is(err, ErrRecordInvalid) {
			t.Fatalf("unsafe removal %q: %v", relative, err)
		}
	}
}

func TestScratchSweepDoesNotExpirePendingReview(t *testing.T) {
	t.Parallel()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.Put(bytes.NewBufferString("pending review"))
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := store.CopyTo(t.Context(), source.Record,
		ItemDirectory(uuid.NewString())+"/payload/content", "game.bin")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	old := now.Add(-2 * ScratchLifetime)
	for _, file := range []Metadata{source, prepared} {
		if err := os.Chtimes(file.Path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SweepWrites(t.Context(), now.Add(-ScratchLifetime)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scratch retained: %v", err)
	}
	if _, err := os.Stat(prepared.Path); err != nil {
		t.Fatalf("pending review removed: %v", err)
	}
	if _, err := ParseRecord(prepared.Record + " {}"); err == nil {
		t.Fatal("accepted trailing record data")
	}
}
