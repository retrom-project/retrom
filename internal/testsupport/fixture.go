package testsupport

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/persistence"
)

type Fixture struct {
	Repository  *persistence.Repository
	Principal   model.Principal
	Directory   model.DirectoryInput
	DirectoryID string
	Game        model.PreparedGame
}

func Library(t *testing.T) Fixture {
	t.Helper()
	r := Database(t)
	user := model.User{
		ID: uuid.NewString(), Username: "administrator", DisplayName: "Administrator",
		Role: "admin", Status: "active",
	}
	directory := model.DirectoryInput{
		PlatformID: "nes", Name: "Test", Slug: "test", DefaultCoreID: "fceumm",
		CoreIDs: []string{"fceumm"}, Enabled: true,
	}
	id := uuid.NewString()
	err := r.Transaction(t.Context(), func(tx *persistence.Repository) error {
		if err := tx.Initialize(t.Context(), user, "test-hash", 1000); err != nil {
			return fmt.Errorf("prepare library fixture: %w", err)
		}
		return tx.WriteDirectory(t.Context(), id, directory, 1000, true)
	})
	if err != nil {
		t.Fatal(err)
	}
	game := model.PreparedGame{
		ID: uuid.NewString(), ContentHash: "content-hash",
		Input: model.GameInput{
			PlatformInstanceID: id, Title: "Test game",
			RuntimeConfig: json.RawMessage(`{"content":{"kind":"SINGLE_FILE","entryFile":"game.nes"}}`),
			TagIDs:        []string{},
		}, Files: []model.GameFile{{
			ID: uuid.NewString(), LogicalKey: "game.nes", Role: "ROM",
			StorageKey: "unused-fixture-key", SizeBytes: 3, SHA256: "content-hash",
		}},
	}
	return Fixture{
		Repository: r, Principal: model.Principal{User: user}, Directory: directory, DirectoryID: id,
		Game: game,
	}
}

func (f Fixture) Publish(t *testing.T) {
	t.Helper()
	err := f.Repository.Transaction(t.Context(), func(tx *persistence.Repository) error {
		if err := tx.CreateGame(t.Context(), f.Principal.User.ID, f.Game, 1000); err != nil {
			return fmt.Errorf("prepare library fixture: %w", err)
		}
		return tx.TransitionGame(t.Context(), f.Game.ID, "pending_review", "published", 1, 1000)
	})
	if err != nil {
		t.Fatal(err)
	}
}
