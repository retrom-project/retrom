//go:build integration

package library

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"retrom/internal/model"
	"retrom/internal/persistence"
)

func gameUpdate(detail model.GameDetail) model.GameInput {
	game := detail.Game
	return model.GameInput{
		PlatformInstanceID: game.PlatformInstanceID, Title: game.Title, Description: game.Description,
		Developer: game.Developer, Publisher: game.Publisher, Genre: game.Genre, Players: game.Players, ReleaseYear: game.ReleaseYear,
		RuntimeConfig: detail.RuntimeConfig, Version: game.Version, TagIDs: []string{},
	}
}

func TestDATSelectionRetiresParentOnlyAfterItsLastCoreReferenceIsRemoved(t *testing.T) {
	t.Parallel()
	s, f, _ := parentFixture(t)
	cores := []string{"fbneo", "mame2003", "mame2003_plus", "mame_arcade"}
	directory, err := s.Repository.Directory(t.Context(), f.DirectoryID)
	if err != nil {
		t.Fatal(err)
	}
	directory.CoreIDs = cores
	inputDirectory := f.Directory
	inputDirectory.CoreIDs, inputDirectory.Version = cores, directory.Version
	if err := s.Repository.WriteDirectory(t.Context(), f.DirectoryID, inputDirectory, 2000, false); err != nil {
		t.Fatal(err)
	}
	before, err := s.Detail(t.Context(), f.Principal, f.Game.ID, "pending_review")
	if err != nil {
		t.Fatal(err)
	}
	parent := parentFile(t, before, "1941.zip")
	config := map[string]any{"content": map[string]string{"kind": "ARCADE", "entryFile": "1941j.zip"}}
	selections := make(map[string]any)
	for _, core := range cores {
		selections[core] = map[string]any{"romSet": "1941j", "parentFiles": []string{"1941.zip"}}
	}
	config["cores"] = selections
	current := before
	save := func() {
		t.Helper()
		input := gameUpdate(current)
		input.RuntimeConfig, err = json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		current, err = s.Update(t.Context(), f.Principal, f.Game.ID, "pending_review", input)
		if err != nil {
			t.Fatal(err)
		}
	}
	save()
	for index, core := range cores {
		selections[core] = map[string]any{"romSet": "1941", "parentFiles": []string{"1941.zip"}}
		save()
		expected := 2
		if index == len(cores)-1 {
			expected = 1
		}
		if len(current.Files) != expected {
			t.Fatalf("%s change retired another core's Parent: %+v", core, current.Files)
		}
		assertParentBytesRetained(t, s, parent)
	}
	if current.Files[0].LogicalKey != "1941j.zip" || current.Game.ContentHash != current.Files[0].SHA256 {
		t.Fatalf("entry or identity lost after retirement: %+v", current)
	}
	count, err := s.Repository.Count(t.Context(), `SELECT count(*) FROM game_file_tab WHERE id=$1 AND status='deleted'`, parent.ID)
	if err != nil || count != 1 {
		t.Fatalf("obsolete parent was not queued for normal cleanup: %d %v", count, err)
	}
}

func TestDATSelectionCASFailureKeepsParentConfigurationAndBytes(t *testing.T) {
	t.Parallel()
	s, f, _ := parentFixture(t)
	before, err := s.Detail(t.Context(), f.Principal, f.Game.ID, "pending_review")
	if err != nil {
		t.Fatal(err)
	}
	input := gameUpdate(before)
	input.RuntimeConfig = json.RawMessage(`{"content":{"kind":"ARCADE","entryFile":"1941j.zip"},"cores":{"mame2003":{"romSet":"1941","parentFiles":["1941.zip"]}}}`)
	validate := s.ValidateConfig
	s.ValidateConfig = func(ctx context.Context, platform string, config json.RawMessage, files []model.ContentFile) error {
		if err := validate(ctx, platform, config, files); err != nil {
			return err
		}
		return s.Repository.BumpGame(ctx, f.Game.ID, before.Game.Version, 2000)
	}
	if _, err := s.Update(t.Context(), f.Principal, f.Game.ID, "pending_review", input); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale selection save=%v", err)
	}
	after, err := s.Detail(t.Context(), f.Principal, f.Game.ID, "pending_review")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.Files, before.Files) || !reflect.DeepEqual(after.RuntimeConfig, before.RuntimeConfig) || after.Game.ContentHash != before.Game.ContentHash {
		t.Fatalf("CAS failure retired a Parent or changed configuration: %+v", after)
	}
	for _, file := range after.Files {
		absolute, err := s.Storage.Absolute(file.StorageKey)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(absolute); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDATSelectionRetirementFailureRollsBackConfigVersionAndHash(t *testing.T) {
	t.Parallel()
	s, f, _ := parentFixture(t)
	before, err := s.Detail(t.Context(), f.Principal, f.Game.ID, "pending_review")
	if err != nil {
		t.Fatal(err)
	}
	input := gameUpdate(before)
	input.RuntimeConfig = json.RawMessage(`{"content":{"kind":"ARCADE","entryFile":"1941j.zip"},"cores":{"mame2003":{"romSet":"1941"}}}`)
	err = s.Repository.Transaction(t.Context(), func(r *persistence.Repository) error {
		if err := r.UpdateGame(t.Context(), f.Game.ID, f.Principal.User.ID, "pending_review", input, 2000); err != nil {
			return err
		}
		return r.RetireParentFiles(t.Context(), f.Game.ID, "changed", []string{"absent-file"}, 2000)
	})
	if !errors.Is(err, model.ErrConflict) {
		t.Fatalf("retirement failure=%v", err)
	}
	after, err := s.Detail(t.Context(), f.Principal, f.Game.ID, "pending_review")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("failed retirement left partial state: %+v %v", after, err)
	}
}

func assertParentBytesRetained(t *testing.T, s *Service, parent model.GameFile) {
	t.Helper()
	absolute, err := s.Storage.Absolute(parent.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(absolute); err != nil {
		t.Fatalf("Parent bytes removed before cleanup grace: %v", err)
	}
}
