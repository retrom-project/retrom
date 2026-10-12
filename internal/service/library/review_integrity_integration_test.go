//go:build integration

package library

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/testsupport"
)

func TestApproveRechecksMissingDATMembersOnTheServer(t *testing.T) {
	t.Parallel()
	s, f, _ := parentFixture(t)
	if _, err := s.Approve(t.Context(), f.Principal, f.Game.ID, 1, ""); err == nil {
		t.Fatal("incomplete DAT game was published through direct approval")
	}
	if _, err := s.Detail(t.Context(), f.Principal, f.Game.ID, "published"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("incomplete game became public: %v", err)
	}
}

func TestWithdrawPreservesGameAndPrivateRelationsWithVersionCAS(t *testing.T) {
	t.Parallel()
	f := testsupport.Library(t)
	f.Publish(t)
	s := &Service{Repository: f.Repository, Now: func() time.Time { return time.UnixMilli(3000) }}
	ctx := t.Context()
	saved := withdrawPrivateFixture(t, f)
	before, err := s.Detail(ctx, f.Principal, f.Game.ID, "published")
	if err != nil {
		t.Fatal(err)
	}
	ordinary := f.Principal
	ordinary.User.Role = "user"
	if _, err = s.Withdraw(ctx, ordinary, f.Game.ID, 2); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("ordinary withdrawal=%v", err)
	}
	if _, err = s.Withdraw(ctx, f.Principal, f.Game.ID, 1); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale withdrawal=%v", err)
	}
	after, err := s.Withdraw(ctx, f.Principal, f.Game.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertWithdrawRetained(t, f, before, after, saved)
	if _, err = s.Detail(ctx, ordinary, f.Game.ID, "published"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("pending game is public: %v", err)
	}
	if _, err = s.Detail(ctx, ordinary, f.Game.ID, "pending_review"); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("ordinary review=%v", err)
	}
	page, err := s.List(ctx, ordinary, "published", model.Query{Limit: 24})
	if err != nil || page.Total != 0 {
		t.Fatalf("withdrawn game listed: %+v %v", page, err)
	}
	if _, err = s.Withdraw(ctx, f.Principal, f.Game.ID, 3); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("repeated withdrawal=%v", err)
	}
	// The publication state is the only restored field; private data comes back with the same ID.
	if err = f.Repository.TransitionGame(ctx, f.Game.ID, "pending_review", "published", 3, 4000); err != nil {
		t.Fatal(err)
	}
	restored, err := s.Detail(ctx, ordinary, f.Game.ID, "published")
	if err != nil || !restored.Game.Favorite || restored.Game.LastPlayedAtMs == nil || *restored.Game.LastPlayedAtMs != 2000 {
		t.Fatalf("relations not retained: %+v %v", restored.Game, err)
	}
}

func withdrawPrivateFixture(t *testing.T, f testsupport.Fixture) model.Save {
	t.Helper()
	ctx := t.Context()
	if err := f.Repository.SetFavorite(ctx, f.Principal.User.ID, f.Game.ID, []string{}, 2000); err != nil {
		t.Fatal(err)
	}
	if err := f.Repository.RecordRunning(ctx, f.Principal.User.ID, f.Game.ID, 2000); err != nil {
		t.Fatal(err)
	}
	save := model.Save{
		ID: uuid.NewString(), UserID: f.Principal.User.ID, Game: model.Game{ID: f.Game.ID}, Kind: "checkpoint",
		Name: "Private save", SizeBytes: 3, StorageKey: "saves/original", PayloadHash: "original", LastCommitID: uuid.NewString(),
		Extinfo: model.Extinfo{
			CoreID: "fceumm", ProviderID: "emulatorjs", TargetID: "fceumm", CoreFingerprint: "fingerprint", ROMHash: f.Game.ContentHash,
			CheckpointFormat: "format", Content: json.RawMessage(`{"kind":"SINGLE_FILE","entryFile":"game.nes"}`), RuntimeOptions: json.RawMessage(`{}`),
		},
	}
	if err := f.Repository.WriteSave(ctx, save, 2000, true); err != nil {
		t.Fatal(err)
	}
	saved, err := f.Repository.Save(ctx, f.Principal.User.ID, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func assertWithdrawRetained(t *testing.T, f testsupport.Fixture, before, after model.GameDetail, saved model.Save) {
	t.Helper()
	ctx := t.Context()
	if after.Game.Version != 3 || after.Game.Status != "pending_review" || after.Game.UpdatedAtMs != 3000 {
		t.Fatalf("transition=%+v", after.Game)
	}
	before.Game.Version, before.Game.Status, before.Game.UpdatedAtMs = after.Game.Version, after.Game.Status, after.Game.UpdatedAtMs
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("withdrawal changed content or private facts: before=%+v after=%+v", before, after)
	}
	sameSave, err := f.Repository.Save(ctx, f.Principal.User.ID, saved.ID)
	if err != nil || !reflect.DeepEqual(saved.Extinfo, sameSave.Extinfo) || sameSave.StorageKey != saved.StorageKey || sameSave.Version != saved.Version {
		t.Fatalf("private save altered: %+v %v", sameSave, err)
	}
}
