//go:build integration

package persistence_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/testsupport"
)

func TestSaveGameGroupsPaginateUniqueGamesAndRespectOwnership(t *testing.T) {
	f, latest := groupedSavesFixture(t)
	seen := make(map[string]bool)
	for _, offset := range []int{0, 24} {
		page, err := f.Repository.Saves(t.Context(), f.Principal.User.ID, "", model.Query{Limit: 24, Offset: offset, GroupByGame: true})
		wantLength := 24
		if offset == 24 {
			wantLength = 2
		}
		if err != nil || page.Total != 26 || page.GameCount != 26 || len(page.Items) != wantLength {
			t.Fatalf("group page offset=%d total=%d games=%d items=%d error=%v", offset, page.Total, page.GameCount, len(page.Items), err)
		}
		for _, save := range page.Items {
			if seen[save.Game.ID] || latest[save.Game.ID] != save.ID {
				t.Fatalf("duplicate game or wrong latest save: game=%s save=%s", save.Game.ID, save.ID)
			}
			seen[save.Game.ID] = true
		}
	}
	if len(seen) != 26 {
		t.Fatalf("unique games=%d", len(seen))
	}
	page, err := f.Repository.Saves(t.Context(), f.Principal.User.ID, "", model.Query{Limit: 100})
	if err != nil || page.Total != 79 || page.GameCount != 26 || len(page.Items) != 79 {
		t.Fatalf("individual saves changed: total=%d games=%d items=%d error=%v", page.Total, page.GameCount, len(page.Items), err)
	}
}

func TestSaveGameGroupsFilterBeforeChoosingLatestAndSortByTitle(t *testing.T) {
	f, _ := groupedSavesFixture(t)
	for _, query := range []model.Query{
		{Limit: 100, GroupByGame: true, Search: "older progress"},
		{Limit: 100, GroupByGame: true, Kind: "game_save"},
	} {
		page, err := f.Repository.Saves(t.Context(), f.Principal.User.ID, "", query)
		if err != nil || page.Total != 26 || len(page.Items) != 26 {
			t.Fatalf("filtered groups total=%d items=%d error=%v", page.Total, len(page.Items), err)
		}
		for _, save := range page.Items {
			if query.Search != "" && save.Name != query.Search || query.Kind != "" && save.Kind != query.Kind {
				t.Fatalf("selected non-matching save %s", save.ID)
			}
		}
	}
	page, err := f.Repository.Saves(t.Context(), f.Principal.User.ID, "", model.Query{Limit: 1, Offset: 24, Sort: "title", GroupByGame: true})
	if err != nil || page.Total != 26 || len(page.Items) != 1 || page.Items[0].Game.Title != "Game 24" {
		t.Fatalf("title page total=%d items=%d error=%v", page.Total, len(page.Items), err)
	}
}

func groupedSavesFixture(t *testing.T) (testsupport.Fixture, map[string]string) {
	t.Helper()
	f := testsupport.Library(t)
	latest := make(map[string]string)
	for i := range 27 {
		f.Game.ID, f.Game.Files[0].ID = uuid.NewString(), uuid.NewString()
		f.Game.Input.Title = fmt.Sprintf("Game %02d", i)
		f.Publish(t)
		if i == 26 {
			writeGroupSave(t, f, uuid.NewString(), "other user only", "checkpoint", 9000)
			continue
		}
		writeGroupSave(t, f, f.Principal.User.ID, "older progress", "checkpoint", int64(2000+i*10))
		writeGroupSave(t, f, f.Principal.User.ID, "in-game progress", "game_save", int64(2001+i*10))
		latest[f.Game.ID] = writeGroupSave(t, f, f.Principal.User.ID, "latest progress", "checkpoint", int64(2002+i*10))
		if i == 0 {
			tie := writeGroupSave(t, f, f.Principal.User.ID, "timestamp tie", "checkpoint", 2002)
			if tie < latest[f.Game.ID] {
				latest[f.Game.ID] = tie
			}
			writeGroupSave(t, f, uuid.NewString(), "other user's newer save", "checkpoint", 9000)
			deleted := writeGroupSave(t, f, f.Principal.User.ID, "deleted newer save", "checkpoint", 9000)
			if err := f.Repository.DeleteSave(t.Context(), f.Principal.User.ID, deleted, 1, 10000); err != nil {
				t.Fatal(err)
			}
		}
	}
	return f, latest
}

func writeGroupSave(t *testing.T, f testsupport.Fixture, userID, name, kind string, at int64) string {
	t.Helper()
	id := uuid.NewString()
	err := f.Repository.WriteSave(t.Context(), model.Save{
		ID: id, UserID: userID, Game: model.Game{ID: f.Game.ID}, Name: name, Kind: kind,
		StorageKey: "unused-" + id, PayloadHash: "hash-" + id, SizeBytes: 3, LastCommitID: id,
	}, at, true)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
