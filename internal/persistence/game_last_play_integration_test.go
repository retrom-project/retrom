//go:build integration

package persistence_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/testsupport"
)

func TestGameLastPlayProjectionIsCurrentAndPrivate(t *testing.T) {
	f := testsupport.Library(t)
	f.Publish(t)
	ctx, userID, otherID := t.Context(), f.Principal.User.ID, uuid.NewString()
	for _, id := range []string{userID, otherID} {
		if err := f.Repository.SetFavorite(ctx, id, f.Game.ID, nil, 1000); err != nil {
			t.Fatal(err)
		}
	}
	assertGameLastPlayProjections(t, f, userID, nil)
	otherAt := int64(5000)
	if err := f.Repository.RecordRunning(ctx, otherID, f.Game.ID, otherAt); err != nil {
		t.Fatal(err)
	}
	assertGameLastPlayProjections(t, f, userID, nil)
	assertGameLastPlayProjections(t, f, otherID, &otherAt)
	for _, at := range []int64{10000, 20000} {
		if err := f.Repository.RecordRunning(ctx, userID, f.Game.ID, at); err != nil {
			t.Fatal(err)
		}
		assertGameLastPlayProjections(t, f, userID, &at)
		assertGameLastPlayProjections(t, f, otherID, &otherAt)
	}
}

func assertGameLastPlayProjections(t *testing.T, f testsupport.Fixture, userID string, want *int64) {
	t.Helper()
	ctx := t.Context()
	game, err := f.Repository.Game(ctx, userID, f.Game.ID, "published")
	if err != nil {
		t.Fatal(err)
	}
	assertGameLastPlay(t, game, want)
	detail, err := f.Repository.GameDetail(ctx, userID, f.Game.ID, "published")
	if err != nil {
		t.Fatal(err)
	}
	assertGameLastPlay(t, detail.Game, want)
	for _, query := range []model.Query{{Limit: 1}, {Limit: 1, Sort: "recent"}, {Limit: 1, Favorite: true}} {
		page, listErr := f.Repository.Games(ctx, userID, "published", query)
		if listErr != nil || len(page.Items) != 1 {
			t.Fatalf("games=%+v error=%v", page, listErr)
		}
		assertGameLastPlay(t, page.Items[0], want)
	}
	recent, err := f.Repository.RecentGames(ctx, userID, model.Query{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if want == nil {
		if len(recent.Items) != 0 {
			t.Fatalf("unplayed user's recent=%+v", recent)
		}
		return
	}
	if len(recent.Items) != 1 || recent.Items[0].LastPlayedAtMs != *want {
		t.Fatalf("recent=%+v want=%d", recent, *want)
	}
	assertGameLastPlay(t, recent.Items[0].Game, want)
}

func assertGameLastPlay(t *testing.T, game model.Game, want *int64) {
	t.Helper()
	data, err := json.Marshal(game)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	field, ok := fields["lastPlayedAtMs"]
	if !ok {
		t.Fatal("Game JSON omits required lastPlayedAtMs")
	}
	var got *int64
	if err = json.Unmarshal(field, &got); err != nil {
		t.Fatal(err)
	}
	if want == nil {
		if got != nil {
			t.Fatalf("unplayed user's lastPlayedAtMs=%d", *got)
		}
		return
	}
	if got == nil || *got != *want {
		t.Fatalf("lastPlayedAtMs=%s want=%d", field, *want)
	}
}
