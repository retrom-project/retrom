package immersive

import (
	"context"
	"errors"
	model "retrom/internal/model/immersive"
	"testing"
)

type snapshotRepository struct {
	scope model.ReadScope
	reads int
}

func (repository *snapshotRepository) WithRead(_ context.Context, work func(model.ReadScope) error) error {
	repository.reads++
	return work(repository.scope)
}

type platformReader struct {
	model.PlatformReader
	platforms []model.Platform
	featured  []model.FeaturedGame
	games     []model.Game
}

func (reader platformReader) Platforms(context.Context, string) ([]model.Platform, error) {
	return reader.platforms, nil
}

func (reader platformReader) Platform(context.Context, string, string) (model.Platform, error) {
	return reader.platforms[0], nil
}

func (reader platformReader) Featured(context.Context, string, string) ([]model.FeaturedGame, error) {
	return reader.featured, nil
}

func (reader platformReader) Games(context.Context, string, string, int, *model.GameCursor) ([]model.Game, error) {
	return reader.games, nil
}

func TestPlatformsAttachOnlyMatchingFeaturedGames(t *testing.T) {
	t.Parallel()
	repository := &snapshotRepository{scope: model.ReadScope{Platforms: platformReader{
		platforms: []model.Platform{{ID: "gba"}, {ID: "nes"}}, featured: []model.FeaturedGame{{PlatformID: "gba", ID: "first"}, {PlatformID: "unknown", ID: "hidden"}},
	}}}
	result, err := New(repository).Platforms(t.Context(), "profile")
	if err != nil {
		t.Fatal(err)
	}
	if repository.reads != 1 {
		t.Fatalf("snapshots = %d", repository.reads)
	}
	if len(result[0].FeaturedGames) != 1 || result[0].FeaturedGames[0].ID != "first" {
		t.Fatalf("featured games = %#v", result)
	}
	if result[1].FeaturedGames == nil || len(result[1].FeaturedGames) != 0 {
		t.Fatalf("empty platform = %#v", result[1])
	}
}

func TestGamePageUsesLastVisibleRowForCursor(t *testing.T) {
	t.Parallel()
	repository := &snapshotRepository{scope: model.ReadScope{Platforms: platformReader{
		platforms: []model.Platform{{ID: "gba"}}, games: []model.Game{{ID: "a", Title: "A", TitleInitial: "A"}, {ID: "b", Title: "B", TitleInitial: "B"}, {ID: "c", Title: "C", TitleInitial: "C"}},
	}}}
	result, err := New(repository).Games(t.Context(), "profile", "gba", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 || result.NextCursor == nil || result.NextCursor.ID != "b" {
		t.Fatalf("page = %#v", result)
	}
	if repository.reads != 1 {
		t.Fatalf("snapshots = %d", repository.reads)
	}
}

func TestInvalidLibraryAndFolderDoNotOpenSnapshot(t *testing.T) {
	t.Parallel()
	repository := &snapshotRepository{}
	service := New(repository)
	if _, err := service.LibraryGames(t.Context(), "profile", "unknown", "", model.PageLimit, nil); !errors.Is(err, model.ErrLibraryNotFound) {
		t.Fatalf("unknown library: %v", err)
	}
	if _, err := service.LibraryGames(t.Context(), "profile", model.LibraryRecent, "folder", model.PageLimit, nil); !errors.Is(err, model.ErrFavoriteFolderNotFound) {
		t.Fatalf("invalid folder: %v", err)
	}
	if repository.reads != 0 {
		t.Fatalf("invalid query opened %d snapshots", repository.reads)
	}
}

func TestRecentPageCursorRetainsLastPlayedTime(t *testing.T) {
	t.Parallel()
	recent := int64(2000)
	earlier := int64(1000)
	items, next := libraryPageItems([]model.Game{{ID: "a", LastPlayedAtMS: &recent}, {ID: "b", LastPlayedAtMS: &earlier}}, 1, model.LibraryRecent)
	if len(items) != 1 || next == nil || next.LastPlayedAtMS == nil || *next.LastPlayedAtMS != recent {
		t.Fatalf("recent cursor = %#v", next)
	}
}
