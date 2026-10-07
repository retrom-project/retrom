//go:build integration

package saves

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/testsupport"
)

func TestConcurrentSaveCASAndDeletedGameFence(t *testing.T) {
	f := testsupport.Library(t)
	f.Publish(t)
	s := &Service{Repository: f.Repository, Now: time.Now}
	save := createCommitFixture(t, f, s)
	current := raceConcurrentCommit(t, s, save)
	assertCommitRetryAndDeletionFence(t, f, s, current)
}

func createCommitFixture(t *testing.T, f testsupport.Fixture, s *Service) model.Save {
	t.Helper()
	save := model.Save{ID: uuid.NewString(), UserID: f.Principal.User.ID, Game: model.Game{ID: f.Game.ID}, Kind: "checkpoint", Name: "First", SizeBytes: 3, StorageKey: "saves/first", PayloadHash: "first", LastCommitID: uuid.NewString(), Extinfo: model.Extinfo{CoreID: "fceumm", ProviderID: "emulatorjs", TargetID: "fceumm", CoreFingerprint: "fingerprint", ROMHash: f.Game.ContentHash, CheckpointFormat: "format", Content: json.RawMessage(`{"kind":"SINGLE_FILE","entryFile":"game.nes"}`), RuntimeOptions: json.RawMessage(`{}`)}}
	if err := s.commit(t.Context(), save, true); err != nil {
		t.Fatal(err)
	}
	current, err := f.Repository.Save(t.Context(), save.UserID, save.ID)
	if err != nil || current.Version != 1 {
		t.Fatalf("created=%+v error=%v", current, err)
	}
	return current
}

func raceConcurrentCommit(t *testing.T, s *Service, save model.Save) model.Save {
	t.Helper()
	save.Version = 1
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := range 2 {
		candidate := save
		candidate.LastCommitID = uuid.NewString()
		candidate.PayloadHash = candidate.LastCommitID
		candidate.StorageKey = "saves/" + candidate.LastCommitID
		candidate.Name = []string{"Second", "Third"}[i]
		go func() { <-start; results <- s.commit(t.Context(), candidate, false) }()
	}
	close(start)
	success, conflicts := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			success++
		case errors.Is(err, model.ErrConflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
	current, err := s.Repository.Save(t.Context(), save.UserID, save.ID)
	if err != nil || current.Version != 2 {
		t.Fatalf("overwritten=%+v error=%v", current, err)
	}
	return current
}

func assertCommitRetryAndDeletionFence(t *testing.T, f testsupport.Fixture, s *Service, current model.Save) {
	t.Helper()
	// Repeating the winning domain commit must not create another version.
	if err := s.commit(t.Context(), current, false); err != nil {
		t.Fatal(err)
	}
	after, err := f.Repository.Save(t.Context(), current.UserID, current.ID)
	if err != nil || after.Version != 2 {
		t.Fatalf("retry=%+v error=%v", after, err)
	}
	if err = f.Repository.TransitionGame(t.Context(), f.Game.ID, "published", "deleted", 2, 3000); err != nil {
		t.Fatal(err)
	}
	current.Version, current.LastCommitID = 2, uuid.NewString()
	if err = s.commit(t.Context(), current, false); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("deleted overwrite=%v", err)
	}
	newSave := current
	newSave.ID = uuid.NewString()
	if err = s.commit(t.Context(), newSave, true); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("deleted create=%v", err)
	}
	if count, err := f.Repository.Count(t.Context(), "SELECT count(*) FROM save_tab"); err != nil || count != 1 {
		t.Fatalf("saves=%d error=%v", count, err)
	}
}
