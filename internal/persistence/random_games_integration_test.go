//go:build integration

package persistence_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/testsupport"
)

func TestRandomGamesSampleAllVisibleGamesWithoutDuplicates(t *testing.T) {
	t.Parallel()
	f := testsupport.Library(t)
	titles := make(map[string]string)
	for index := range 20 {
		titles[fmt.Sprintf("Discovery %02d", index)] = ""
	}
	visible := publishTitles(t, f, titles)
	addHiddenDiscoveryGames(t, f)
	for _, limit := range []int{6, 12} {
		first := seededDiscovery(t, f, limit, 0.25)
		seen := make(map[string]bool)
		for _, game := range first.Items {
			if visible[game.Title] != game.ID || seen[game.ID] {
				t.Fatalf("invisible or repeated game: %+v", game)
			}
			seen[game.ID] = true
		}
		ids := func(page model.Page[model.Game]) []string {
			result := make([]string, len(page.Items))
			for index, game := range page.Items {
				result[index] = game.ID
			}
			return result
		}
		if !slices.Equal(ids(first), ids(seededDiscovery(t, f, limit, 0.25))) {
			t.Fatal("a fixed database random seed did not reproduce the sample")
		}
		if slices.Equal(ids(first), ids(seededDiscovery(t, f, limit, 0.75))) {
			t.Fatal("random selection ignored the database random seed")
		}
	}
}

func seededDiscovery(t *testing.T, f testsupport.Fixture, limit int, seed float64) model.Page[model.Game] {
	t.Helper()
	var page model.Page[model.Game]
	err := f.Repository.Transaction(t.Context(), func(tx *persistence.Repository) error {
		if err := tx.Execute(t.Context(), "SELECT setseed($1)", seed); err != nil {
			return err
		}
		var err error
		page, err = tx.Games(t.Context(), f.Principal.User.ID, "published", model.Query{Limit: limit, Sort: "random"})
		return err
	})
	if err != nil || len(page.Items) != limit || page.Total != 20 {
		t.Fatalf("random games=%+v error=%v", page, err)
	}
	return page
}

func addHiddenDiscoveryGames(t *testing.T, f testsupport.Fixture) {
	t.Helper()
	err := f.Repository.Transaction(t.Context(), func(tx *persistence.Repository) error {
		if err := tx.CreateGame(t.Context(), f.Principal.User.ID, f.Game, 1000); err != nil {
			return err
		}
		directory := f.Directory
		directory.Name, directory.Slug, directory.Enabled = "Disabled", "disabled", false
		id := uuid.NewString()
		if err := tx.WriteDirectory(t.Context(), id, directory, 1000, true); err != nil {
			return err
		}
		game := f.Game
		game.ID, game.Input.PlatformInstanceID, game.Files[0].ID = uuid.NewString(), id, uuid.NewString()
		if err := tx.CreateGame(t.Context(), f.Principal.User.ID, game, 1000); err != nil {
			return err
		}
		return tx.TransitionGame(t.Context(), game.ID, "pending_review", "published", 1, 1000)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRandomGamesHandleEmptyAndSmallLibrariesAndFilters(t *testing.T) {
	t.Parallel()
	f := testsupport.Library(t)
	query := model.Query{Limit: 12, Sort: "random"}
	page, err := f.Repository.Games(t.Context(), f.Principal.User.ID, "published", query)
	if err != nil || len(page.Items) != 0 || page.Total != 0 {
		t.Fatalf("empty games=%+v error=%v", page, err)
	}
	publishTitles(t, f, map[string]string{"Alpha": "", "Beta": ""})
	page, err = f.Repository.Games(t.Context(), f.Principal.User.ID, "published", query)
	if err != nil || len(page.Items) != 2 || page.Total != 2 || page.Items[0].ID == page.Items[1].ID {
		t.Fatalf("small games=%+v error=%v", page, err)
	}
	query.Search, query.PlatformID, query.DirectoryID = "Beta", "nes", f.DirectoryID
	page, err = f.Repository.Games(t.Context(), f.Principal.User.ID, "published", query)
	if err != nil || len(page.Items) != 1 || page.Total != 1 || page.Items[0].Title != "Beta" {
		t.Fatalf("filtered games=%+v error=%v", page, err)
	}
}
