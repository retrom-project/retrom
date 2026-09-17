package home

import (
	"context"
	"errors"
	model "retrom/internal/model/home"
	"strings"
	"testing"

	"retrom/internal/model/tagging"
)

type repositoryStub struct {
	summary      model.Summary
	summaryErr   error
	recentGames  []model.RecentGame
	recentErr    error
	recentSaves  []model.RecentSave
	savesErr     error
	latestGames  []model.LatestGame
	latestErr    error
	featured     model.FeaturedGame
	featuredOK   bool
	featuredErr  error
	platforms    []model.Platform
	platformsErr error
	include      bool
}

func (stub *repositoryStub) Summary(context.Context, string) (model.Summary, error) {
	return stub.summary, stub.summaryErr
}

func (stub *repositoryStub) RecentSaves(context.Context, string) ([]model.RecentSave, error) {
	return stub.recentSaves, stub.savesErr
}

func (stub *repositoryStub) RecentGames(_ context.Context, _ string, includeDeleted bool) ([]model.RecentGame, error) {
	stub.include = includeDeleted
	return stub.recentGames, stub.recentErr
}

func (stub *repositoryStub) LatestGames(context.Context) ([]model.LatestGame, error) {
	return stub.latestGames, stub.latestErr
}

func (stub *repositoryStub) FeaturedGame(context.Context, string) (model.FeaturedGame, bool, error) {
	return stub.featured, stub.featuredOK, stub.featuredErr
}

func (stub *repositoryStub) Platforms(context.Context, string) ([]model.Platform, error) {
	return stub.platforms, stub.platformsErr
}

type tagReaderStub struct {
	references map[string][]tagging.Reference
	err        error
	ids        [][]string
}

func (stub *tagReaderStub) References(_ context.Context, ids []string) (map[string][]tagging.Reference, error) {
	stub.ids = append(stub.ids, append([]string(nil), ids...))
	return stub.references, stub.err
}

func TestDashboardSortsQuickPlatformsWithoutChangingCatalogOrder(t *testing.T) {
	repository := &repositoryStub{platforms: []model.Platform{
		{ID: "z", Name: "Zulu", PlayCount: 2},
		{ID: "b", Name: "Bravo", PlayCount: 4},
		{ID: "a", Name: "Alpha", PlayCount: 4},
		{ID: "c", Name: "Charlie", PlayCount: 3},
		{ID: "d", Name: "Delta", PlayCount: 1},
	}}
	result, err := New(repository, nil).Dashboard(context.Background(), "profile")
	if err != nil {
		t.Fatal(err)
	}
	if repository.include {
		t.Fatal("dashboard should exclude deleted games")
	}
	if len(result.QuickPlatforms) != 4 {
		t.Fatalf("quick platforms = %#v, want four items", result.QuickPlatforms)
	}
	wantQuick := []string{"a", "b", "c", "z"}
	for index, want := range wantQuick {
		if result.QuickPlatforms[index].ID != want {
			t.Fatalf("quick platform %d = %q, want %q", index, result.QuickPlatforms[index].ID, want)
		}
	}
	if result.Platforms[0].ID != "z" || result.Platforms[1].ID != "b" {
		t.Fatalf("catalog order changed: %#v", result.Platforms)
	}
}

func TestRecentGamesProjectsEmptyTagsAndWrapsTagErrors(t *testing.T) {
	repository := &repositoryStub{recentGames: []model.RecentGame{{GameID: "game"}}}
	tags := &tagReaderStub{references: map[string][]tagging.Reference{}}
	games, err := New(repository, tags).RecentGames(context.Background(), "profile", true)
	if err != nil {
		t.Fatal(err)
	}
	if !repository.include || len(games) != 1 || games[0].Tags == nil {
		t.Fatalf("games = %#v, includeDeleted = %v", games, repository.include)
	}

	cause := errors.New("tag lookup failed")
	_, err = New(repository, &tagReaderStub{err: cause}).RecentGames(context.Background(), "profile", false)
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "project home tags") {
		t.Fatalf("tag error = %v", err)
	}
}

func TestDashboardWrapsRepositoryErrors(t *testing.T) {
	cause := errors.New("summary unavailable")
	_, err := New(&repositoryStub{summaryErr: cause}, nil).Dashboard(context.Background(), "profile")
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "read home summary") {
		t.Fatalf("summary error = %v", err)
	}
}
