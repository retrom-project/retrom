//go:build integration

package persistence_test

import (
	"testing"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/testsupport"
)

func TestGameAndRecentPaginationSortByCurrentUsersLastPlay(t *testing.T) {
	f, ids := lastPlayedFixture(t)
	assertCurrentUserGameOrder(t, f, ids)
	assertGamePlatformAndDirectoryPage(t, f, ids)
	assertRecentDateBoundaries(t, f, ids)
}

func lastPlayedFixture(t *testing.T) (testsupport.Fixture, []string) {
	t.Helper()
	f := testsupport.Library(t)
	ctx, userID := t.Context(), f.Principal.User.ID
	ids := make([]string, 3)
	for i, title := range []string{"Alpha", "Beta", "Gamma"} {
		f.Game.ID, f.Game.Files[0].ID = uuid.NewString(), uuid.NewString()
		f.Game.Input.Title = title
		f.Publish(t)
		ids[i] = f.Game.ID
	}
	for i, value := range []struct {
		user, game string
		at         int64
	}{
		{userID, ids[0], 2000}, {userID, ids[1], 3000}, {uuid.NewString(), ids[2], 9000},
	} {
		if err := f.Repository.RecordRunning(ctx, value.user, value.game, value.at); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	return f, ids
}

func assertCurrentUserGameOrder(t *testing.T, f testsupport.Fixture, ids []string) {
	t.Helper()
	ctx, userID := t.Context(), f.Principal.User.ID
	page, err := f.Repository.Games(ctx, userID, "published", model.Query{Limit: 10, Sort: "recent"})
	if err != nil || len(page.Items) != 3 || page.Total != 3 {
		t.Fatalf("games=%+v error=%v", page, err)
	}
	for i, want := range []string{ids[1], ids[0], ids[2]} {
		if page.Items[i].ID != want {
			t.Fatalf("game[%d]=%s want=%s", i, page.Items[i].ID, want)
		}
	}
}

func assertGamePlatformAndDirectoryPage(t *testing.T, f testsupport.Fixture, ids []string) {
	t.Helper()
	ctx, userID := t.Context(), f.Principal.User.ID
	page, err := f.Repository.Games(ctx, userID, "published", model.Query{Limit: 1, Offset: 1, Sort: "recent", PlatformID: "nes", DirectoryID: f.DirectoryID})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != ids[0] || page.Total != 3 {
		t.Fatalf("paged games=%+v error=%v", page, err)
	}
	page, err = f.Repository.Games(ctx, userID, "published", model.Query{Limit: 1, PlatformID: "gba", DirectoryID: f.DirectoryID})
	if err != nil || len(page.Items) != 0 || page.Total != 0 {
		t.Fatalf("platform AND=%+v error=%v", page, err)
	}
}

func assertRecentDateBoundaries(t *testing.T, f testsupport.Fixture, ids []string) {
	t.Helper()
	ctx, userID := t.Context(), f.Principal.User.ID
	recent, err := f.Repository.RecentGames(ctx, userID, model.Query{Limit: 1, Offset: 1, Sort: "title"})
	if err != nil || len(recent.Items) != 1 || recent.Items[0].Game.ID != ids[1] || recent.Total != 2 {
		t.Fatalf("recent title=%+v error=%v", recent, err)
	}
	boundary := int64(3000)
	recent, err = f.Repository.RecentGames(ctx, userID, model.Query{Limit: 10, AfterMs: &boundary, BeforeMs: &boundary, Search: "Beta"})
	if err != nil || recent.Total != 1 || len(recent.Items) != 1 || recent.Items[0].LastPlayedAtMs != boundary {
		t.Fatalf("recent inclusive range=%+v error=%v", recent, err)
	}
	boundary = 0
	recent, err = f.Repository.RecentGames(ctx, userID, model.Query{Limit: 10, BeforeMs: &boundary})
	if err != nil || recent.Total != 0 || len(recent.Items) != 0 {
		t.Fatalf("recent before zero=%+v error=%v", recent, err)
	}
}
