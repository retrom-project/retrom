//go:build integration

package library

import (
	"encoding/json"
	"strings"
	"testing"

	"retrom/internal/model"

	"github.com/google/uuid"
)

func TestApprovePackageChecksOwnershipAfterLogicalReadiness(t *testing.T) {
	s, f, _ := parentFixture(t)
	id := uuid.NewString()
	directoryID := uuid.NewString()
	directoryInput := model.DirectoryInput{
		PlatformID: "kirikiri", Name: "Project", Slug: "project",
		DefaultCoreID: "kirikiri2", CoreIDs: []string{"kirikiri2"}, Enabled: true,
	}
	if err := s.Repository.WriteDirectory(t.Context(), directoryID, directoryInput, 1000, true); err != nil {
		t.Fatal(err)
	}
	source, err := s.Storage.Stage(t.Context(), id, strings.NewReader("// valid logical member"))
	if err != nil {
		t.Fatal(err)
	}
	logical := []model.ContentFile{{LogicalKey: "startup.tjs", StorageKey: source.Key, SHA256: source.SHA256, SizeBytes: source.Size}}
	file, err := s.Storage.BuildPackage(t.Context(), id, logical)
	if err != nil {
		t.Fatal(err)
	}
	game := f.Game
	game.ID = id
	game.Files = []model.GameFile{file}
	game.Input.PlatformInstanceID = directoryID
	game.Input.RuntimeConfig = json.RawMessage(`{"content":{"kind":"KIRIKIRI_PROJECT","entryFile":"startup.tjs"}}`)
	if err = s.Repository.CreateGame(t.Context(), f.Principal.User.ID, game, 1000); err != nil {
		t.Fatal(err)
	}
	approved, err := s.Approve(t.Context(), f.Principal, id, 1, "kirikiri2")
	if err != nil || approved.Game.Status != "published" || len(approved.Files) != 1 || approved.Files[0].Role != "project_package" {
		t.Fatalf("package approval=%+v err=%v", approved, err)
	}
}
