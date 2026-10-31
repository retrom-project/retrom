//go:build integration

package library

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"retrom/internal/model"

	"github.com/google/uuid"
)

func TestKirikiriEntryProjectionUsesPackageMembersWithoutChangingSavedConfiguration(t *testing.T) {
	s, principal, id := kirikiriEntryFixture(t)
	var err error
	for _, status := range []string{"pending_review", "published"} {
		before, readErr := s.Repository.GameRuntimeDetail(t.Context(), principal.User.ID, id, status)
		if readErr != nil {
			t.Fatal(readErr)
		}
		first, pageErr := s.KirikiriEntryCandidates(t.Context(), principal, id, "", "", 1)
		if pageErr != nil || !reflect.DeepEqual(first.Entries, []string{"renamed.bin"}) || first.NextCursor == nil {
			t.Fatalf("first candidate page = %+v, %v", first, pageErr)
		}
		last, pageErr := s.KirikiriEntryCandidates(t.Context(), principal, id, *first.NextCursor, "", 1)
		if pageErr != nil || !reflect.DeepEqual(last.Entries, []string{"startup.tjs"}) || last.NextCursor != nil {
			t.Fatalf("last candidate page = %+v, %v", last, pageErr)
		}
		after, readErr := s.Repository.GameRuntimeDetail(t.Context(), principal.User.ID, id, status)
		if readErr != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("candidate read changed immutable game state: %v", readErr)
		}
		if status == "pending_review" {
			if err = s.Repository.TransitionGame(t.Context(), id, status, "published", 1, 1001); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func kirikiriEntryFixture(t *testing.T) (*Service, model.Principal, string) {
	t.Helper()
	s, f, _ := parentFixture(t)
	id, directoryID := uuid.NewString(), uuid.NewString()
	directory := model.DirectoryInput{
		PlatformID: "kirikiri", Name: "Project entries", Slug: "project-entries",
		DefaultCoreID: "kirikiri2", CoreIDs: []string{"kirikiri2"}, Enabled: true,
	}
	if err := s.Repository.WriteDirectory(t.Context(), directoryID, directory, 1000, true); err != nil {
		t.Fatal(err)
	}
	logical := make([]model.ContentFile, 0, 4)
	for _, item := range []struct {
		path string
		data []byte
	}{
		{"startup.tjs", []byte("// entry")},
		{"renamed.bin", []byte{0x58, 0x50, 0x33, 0x0d, 0x0a, 0x20, 0x0a, 0x1a, 0x8b, 0x67, 0x01}},
		{"ordinary.tjs", []byte("// ordinary script")},
		{"image.png", []byte("ordinary image fixture")},
	} {
		source, err := s.Storage.Stage(t.Context(), id, bytes.NewReader(item.data))
		if err != nil {
			t.Fatal(err)
		}
		logical = append(logical, model.ContentFile{
			LogicalKey: item.path, StorageKey: source.Key, SHA256: source.SHA256, SizeBytes: source.Size,
		})
	}
	file, err := s.Storage.BuildPackage(t.Context(), id, logical)
	if err != nil {
		t.Fatal(err)
	}
	game := f.Game
	game.ID, game.Files, game.Input.PlatformInstanceID = id, []model.GameFile{file}, directoryID
	game.Input.RuntimeConfig = json.RawMessage(`{"content":{"kind":"KIRIKIRI_PROJECT","entryFile":"obsolete.xp3"}}`)
	if err = s.Repository.CreateGame(t.Context(), f.Principal.User.ID, game, 1000); err != nil {
		t.Fatal(err)
	}
	return s, f.Principal, id
}

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
