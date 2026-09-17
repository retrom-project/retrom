package favorites

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/favorites"
)

type favoriteRepositoryStub struct {
	model.Repository
	favoriteResult model.State
	favoriteError  error
	commits        int
}

func (repository *favoriteRepositoryStub) CommitFavorite(_ context.Context, _ model.FavoriteCommand) (model.State, error) {
	repository.commits++
	if repository.favoriteError != nil {
		return model.State{}, repository.favoriteError
	}
	return repository.favoriteResult, nil
}

func TestFavoriteRejectsInvalidAndInvisibleGamesBeforeWriting(t *testing.T) {
	t.Parallel()
	repository := &favoriteRepositoryStub{favoriteError: model.ErrGameNotFound}
	service := New(repository, func() time.Time { return time.UnixMilli(2000) })
	if _, err := service.Favorite(t.Context(), model.Principal{ProfileID: "owner"}, "invalid"); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("invalid identity error = %v", err)
	}
	if repository.commits != 0 {
		t.Fatal("invalid input reached repository")
	}
	if _, err := service.Favorite(t.Context(), model.Principal{ProfileID: "owner"}, favoriteBoundaryID('1', 1)); !errors.Is(err, model.ErrGameNotFound) {
		t.Fatalf("hidden game error = %v", err)
	}
	if repository.commits != 1 {
		t.Fatalf("expected one commit, got %d", repository.commits)
	}
}

func TestFavoriteUsesPrincipalAndClockWithinOneWriteScope(t *testing.T) {
	t.Parallel()
	gameID := favoriteBoundaryID('1', 1)
	repository := &favoriteRepositoryStub{
		favoriteResult: model.State{GameID: gameID, FavoritedAtMS: 2000, FolderIDs: []string{}},
	}
	service := New(repository, func() time.Time { return time.UnixMilli(2000) })
	state, err := service.Favorite(t.Context(), model.Principal{ProfileID: "owner"}, gameID)
	if err != nil {
		t.Fatal(err)
	}
	if repository.commits != 1 {
		t.Fatalf("expected one commit, got %d", repository.commits)
	}
	if state.GameID != gameID || state.FavoritedAtMS != 2000 {
		t.Fatalf("favorite state = %#v", state)
	}
}

func TestFavoritePreservesRepositoryError(t *testing.T) {
	t.Parallel()
	cause := errors.New("database failure")
	repository := &favoriteRepositoryStub{favoriteError: cause}
	service := New(repository, func() time.Time { return time.UnixMilli(2000) })
	gameID := favoriteBoundaryID('1', 1)
	if _, err := service.Favorite(t.Context(), model.Principal{ProfileID: "owner"}, gameID); !errors.Is(err, cause) {
		t.Fatalf("error = %v, want wrapped %v", err, cause)
	}
}
