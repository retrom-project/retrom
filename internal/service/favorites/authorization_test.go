package favorites

import (
	"context"
	"errors"
	"testing"
	"time"
)

type favoriteRepositoryStub struct {
	Repository
	games  FavoriteRecords
	writes int
}

func (repository *favoriteRepositoryStub) WithWrite(_ context.Context, work func(WriteScope) error) error {
	repository.writes++
	return work(WriteScope{Games: repository.games})
}

type favoriteRecordsStub struct {
	FavoriteRecords
	visible           bool
	ensured           bool
	profileID, gameID string
	nowMS             int64
}

func (records *favoriteRecordsStub) Visible(context.Context, string) (bool, error) {
	return records.visible, nil
}

func (records *favoriteRecordsStub) Ensure(_ context.Context, profileID, gameID string, nowMS int64) error {
	records.ensured = true
	records.profileID, records.gameID, records.nowMS = profileID, gameID, nowMS
	return nil
}

func (records *favoriteRecordsStub) State(_ context.Context, profileID, gameID string) (State, bool, error) {
	if profileID != records.profileID || gameID != records.gameID {
		return State{}, false, ErrInvariant
	}
	return State{GameID: gameID, FavoritedAtMS: records.nowMS, FolderIDs: []string{}}, records.ensured, nil
}

func TestFavoriteRejectsInvalidAndInvisibleGamesBeforeWriting(t *testing.T) {
	t.Parallel()
	records := &favoriteRecordsStub{}
	repository := &favoriteRepositoryStub{games: records}
	service := New(repository, func() time.Time { return time.UnixMilli(2000) })
	if _, err := service.Favorite(t.Context(), Principal{ProfileID: "owner"}, "invalid"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid identity error = %v", err)
	}
	if repository.writes != 0 {
		t.Fatal("invalid input reached repository")
	}
	if _, err := service.Favorite(t.Context(), Principal{ProfileID: "owner"}, favoriteBoundaryID('1', 1)); !errors.Is(err, ErrGameNotFound) {
		t.Fatalf("hidden game error = %v", err)
	}
	if records.ensured || repository.writes != 1 {
		t.Fatalf("hidden game wrote=%v, transactions=%d", records.ensured, repository.writes)
	}
}

func TestFavoriteUsesPrincipalAndClockWithinOneWriteScope(t *testing.T) {
	t.Parallel()
	records := &favoriteRecordsStub{visible: true}
	repository := &favoriteRepositoryStub{games: records}
	service := New(repository, func() time.Time { return time.UnixMilli(2000) })
	gameID := favoriteBoundaryID('1', 1)
	state, err := service.Favorite(t.Context(), Principal{ProfileID: "owner"}, gameID)
	if err != nil {
		t.Fatal(err)
	}
	if records.profileID != "owner" || records.gameID != gameID || repository.writes != 1 {
		t.Fatalf("favorite owner=%q, game=%q, writes=%d", records.profileID, records.gameID, repository.writes)
	}
	if state.GameID != gameID || state.FavoritedAtMS != 2000 {
		t.Fatalf("favorite state = %#v", state)
	}
}
