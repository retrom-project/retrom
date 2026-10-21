//go:build integration

package content

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/storage"
	"retrom/internal/testsupport"

	"github.com/google/uuid"
)

type projectRuntime struct{}

func (projectRuntime) StorageLayout(context.Context, model.Directory, json.RawMessage) (string, error) {
	return "ZIP_STORE", nil
}

func TestPackBatchLimitResumesPastCompletedGames(t *testing.T) {
	s, f := packFixture(t)
	for i := range 2 {
		game := f.Game
		game.ID = uuid.NewString()
		game.ContentHash = fmt.Sprintf("distinct-%d", i)
		file, err := s.Storage.Write(t.Context(), "games", game.ID, strings.NewReader("project"), 1024)
		if err != nil {
			t.Fatal(err)
		}
		game.Files = []model.GameFile{{
			ID: uuid.NewString(), Role: "content", LogicalKey: "startup.tjs",
			StorageKey: file.Key, SHA256: file.SHA256, SizeBytes: file.Size,
		}}
		if err = s.Repository.CreateGame(t.Context(), f.Principal.User.ID, game, 1000); err != nil {
			t.Fatal(err)
		}
	}
	for expected := int64(1); expected <= 3; expected++ {
		if err := s.PackBatch(t.Context(), "", 1, true, func(PackResult) error { return nil }); err != nil {
			t.Fatal(err)
		}
		count, err := s.Repository.Count(t.Context(), "SELECT count(*) FROM game_file_tab WHERE status='active' AND role='project_package'")
		if err != nil || count != expected {
			t.Fatalf("batch did not advance: count=%d expected=%d err=%v", count, expected, err)
		}
	}
}

func packFixture(t *testing.T) (*Service, testsupport.Fixture) {
	t.Helper()
	f := testsupport.Library(t)
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	f.Game.Files = nil
	for _, name := range []string{"startup.tjs", "data/中文 #.xp3", "empty.txt"} {
		body := ""
		if name != "empty.txt" {
			body = "unchanged " + name
		}
		file, writeErr := store.Write(t.Context(), "games", f.Game.ID, strings.NewReader(body), 1024)
		if writeErr != nil {
			t.Fatal(writeErr)
		}
		f.Game.Files = append(f.Game.Files, model.GameFile{
			ID: uuid.NewString(), LogicalKey: name, Role: "content",
			StorageKey: file.Key, SHA256: file.SHA256, SizeBytes: file.Size,
		})
	}
	f.Publish(t)
	return &Service{Repository: f.Repository, Storage: store, Runtime: projectRuntime{}, Now: func() time.Time { return time.UnixMilli(5000) }}, f
}

func TestPackExistingPreservesLogicalFactsAndCAS(t *testing.T) {
	s, f := packFixture(t)
	before, err := f.Repository.ContentSnapshot(t.Context(), f.Game.ID)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.PackExisting(t.Context(), f.Game.ID, false)
	if err != nil || preview.Status != "candidate" || preview.Members != 3 {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	unchanged, err := f.Repository.ContentSnapshot(t.Context(), f.Game.ID)
	if err != nil || !reflect.DeepEqual(before, unchanged) {
		t.Fatal("preview modified storage")
	}
	result, err := s.PackExisting(t.Context(), f.Game.ID, true)
	if err != nil || result.Status != "packed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	after, err := f.Repository.ContentSnapshot(t.Context(), f.Game.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertPackedFacts(t, s, before, after)
	result, err = s.PackExisting(t.Context(), f.Game.ID, true)
	if err != nil || result.Status != "already_packed" {
		t.Fatalf("rerun=%+v err=%v", result, err)
	}
	assertOldSnapshotRejected(t, s, before, after.Files[0])
}

func assertOldSnapshotRejected(t *testing.T, s *Service, old persistence.ContentSnapshot, file model.GameFile) {
	t.Helper()
	err := s.Repository.Transaction(t.Context(), func(tx *persistence.Repository) error {
		return tx.SwitchContentStorage(t.Context(), old, file, 6000)
	})
	if !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale storage conversion accepted: %v", err)
	}
}

func TestPackExistingMissingSourceDoesNotChangeGame(t *testing.T) {
	s, f := packFixture(t)
	before, err := f.Repository.ContentSnapshot(t.Context(), f.Game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Storage.Remove(before.Files[0].StorageKey); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PackExisting(t.Context(), f.Game.ID, true); err == nil {
		t.Fatal("accepted missing source")
	}
	after, err := f.Repository.ContentSnapshot(t.Context(), f.Game.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("failure switched storage")
	}
}

func TestPackExistingDoesNotSkipMissingPackage(t *testing.T) {
	s, f := packFixture(t)
	if _, err := s.PackExisting(t.Context(), f.Game.ID, true); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.Repository.ContentSnapshot(t.Context(), f.Game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Storage.Remove(snapshot.Files[0].StorageKey); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PackExisting(t.Context(), f.Game.ID, true); err == nil {
		t.Fatal("missing package reported as complete")
	}
}

func assertPackedFacts(t *testing.T, s *Service, before, after persistence.ContentSnapshot) {
	t.Helper()
	var err error
	if len(after.Files) != 1 || after.Files[0].Role != "project_package" || after.Version != before.Version+1 ||
		after.ContentHash != before.ContentHash || after.Status != before.Status || string(after.Config) != string(before.Config) {
		t.Fatalf("conversion changed game: %+v", after)
	}
	logical, err := s.Repository.ContentFiles(t.Context(), after.Files)
	if err != nil || len(logical) != 3 {
		t.Fatalf("members=%d err=%v", len(logical), err)
	}
	for i, file := range logical {
		original := before.Files[i]
		if file.LogicalKey != original.LogicalKey || file.SHA256 != original.SHA256 || file.SizeBytes != original.SizeBytes {
			t.Fatal("logical facts changed")
		}
		reader, readErr := s.Storage.Read(original.StorageKey)
		if readErr != nil {
			t.Fatal("old run lost retired bytes")
		}
		if closeErr := reader.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}
}
