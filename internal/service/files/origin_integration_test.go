//go:build integration

package files

import (
	"errors"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/storage"
	"retrom/internal/testsupport"

	"github.com/google/uuid"
)

func TestOriginalOwnershipGraceAndGameDeletion(t *testing.T) {
	f, store := testsupport.Library(t), retirementStore(t)
	now := time.Now().Truncate(time.Millisecond)
	old := managedOriginal(t, store, f.Game.ID)
	f.Game.Files = []model.GameFile{old}
	f.Publish(t)
	original := old.ExtInfo.ConversionInfo.OriginInfo.StorageKey
	age(t, store, original, now.Add(-7*24*time.Hour))
	service := &Service{Repository: f.Repository, Storage: store, Now: func() time.Time { return now }, Grace: 24 * time.Hour}
	// A missing owner and an active file ID under the wrong Game must not gain protection.
	for _, ids := range [][2]string{{f.Game.ID, uuid.NewString()}, {uuid.NewString(), old.ID}} {
		orphan := managedOriginalWithID(t, store, ids[0], ids[1])
		key := orphan.ExtInfo.ConversionInfo.OriginInfo.StorageKey
		age(t, store, key, now.Add(-48*time.Hour))
		sweepOrphans(t, service)
		if exists(t, store, key) {
			t.Fatal("orphan or cross-Game attachment was protected")
		}
	}
	if !exists(t, store, original) {
		t.Fatal("active original reclaimed using old physical mtime")
	}
	current := managedOriginal(t, store, f.Game.ID)
	replacement := f.Game
	replacement.Input.Version, replacement.Files = 2, []model.GameFile{current}
	if err := f.Repository.Transaction(t.Context(), func(tx *persistence.Repository) error {
		return tx.ReplaceFiles(t.Context(), f.Game.ID, replacement, now.UnixMilli())
	}); err != nil {
		t.Fatal(err)
	}
	retiredAt := now
	for _, elapsed := range []time.Duration{0, 23 * time.Hour, 25 * time.Hour} {
		now = retiredAt.Add(elapsed)
		if err := service.Sweep(t.Context()); err != nil {
			t.Fatal(err)
		}
		sweepOrphans(t, service)
		want := elapsed < 24*time.Hour
		if exists(t, store, old.StorageKey) != want || exists(t, store, original) != want {
			t.Fatalf("main payload and original disagree with retirement grace at %s", elapsed)
		}
		if !exists(t, store, current.StorageKey) || !exists(t, store, current.ExtInfo.ConversionInfo.OriginInfo.StorageKey) {
			t.Fatal("retiring old row removed current bytes")
		}
	}
	if err := f.Repository.TransitionGame(t.Context(), f.Game.ID, "published", "deleted", 3, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(25 * time.Hour)
	if err := service.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}
	owner, err := store.Absolute("games/" + f.Game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(owner); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted Game retained original attachments: %v", err)
	}
}

func TestOriginalRemovalFailureKeepsDeletedOwnerUntilRetry(t *testing.T) {
	f, store := testsupport.Library(t), retirementStore(t)
	old := managedOriginal(t, store, f.Game.ID)
	f.Game.Files = []model.GameFile{old}
	f.Publish(t)
	replacement := f.Game
	replacement.Input.Version = 2
	replacement.Files = []model.GameFile{managedOriginal(t, store, f.Game.ID)}
	now := time.Now()
	if err := f.Repository.Transaction(t.Context(), func(tx *persistence.Repository) error {
		return tx.ReplaceFiles(t.Context(), f.Game.ID, replacement, now.UnixMilli())
	}); err != nil {
		t.Fatal(err)
	}
	key := old.ExtInfo.ConversionInfo.OriginInfo.StorageKey
	parent, err := store.Absolute(path.Dir(key))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if chmodErr := os.Chmod(parent, 0o700); chmodErr != nil && !errors.Is(chmodErr, os.ErrNotExist) {
			t.Error(chmodErr)
		}
	})
	now = now.Add(25 * time.Hour)
	service := &Service{Repository: f.Repository, Storage: store, Now: func() time.Time { return now }, Grace: 24 * time.Hour}
	if err = service.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}
	count, err := f.Repository.Count(t.Context(), "SELECT count(*) FROM game_file_tab WHERE id=$1 AND status='deleted' AND ext_info<>'{}'::jsonb", old.ID)
	if err != nil || count != 1 || !exists(t, store, key) {
		t.Fatalf("failed original removal lost ownership: rows=%d err=%v", count, err)
	}
	if err = os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = service.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}
	count, err = f.Repository.Count(t.Context(), "SELECT count(*) FROM game_file_tab WHERE id=$1 AND status='purged'", old.ID)
	if err != nil || count != 1 || exists(t, store, key) {
		t.Fatalf("original removal retry failed: rows=%d err=%v", count, err)
	}
}

func managedOriginal(t *testing.T, store *storage.Store, gameID string) model.GameFile {
	t.Helper()
	return managedOriginalWithID(t, store, gameID, uuid.NewString())
}

func managedOriginalWithID(t *testing.T, store *storage.Store, gameID, fileID string) model.GameFile {
	t.Helper()
	main, err := store.Write(t.Context(), "games", gameID, strings.NewReader("runtime content"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	operation := uuid.NewString()
	staged, err := store.Stage(t.Context(), operation, strings.NewReader("original input"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := store.PublishOrigin(t.Context(), gameID, fileID, "raw.zip", staged)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.RemoveTemporary("temporary/" + operation); err != nil {
		t.Fatal(err)
	}
	return model.GameFile{
		ID: fileID, LogicalKey: "game.nes", Role: "content", StorageKey: main.Key, SHA256: main.SHA256, SizeBytes: main.Size,
		ExtInfo: model.GameFileTabExtInfo{ConversionInfo: &model.GameFileConversionInfo{OriginInfo: info}},
	}
}
