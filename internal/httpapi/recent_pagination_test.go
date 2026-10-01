package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"retrom/internal/authn"
	homeservice "retrom/internal/service/home"
)

type recentTestPage struct {
	Items         []recentGameProjection  `json:"items"`
	NextCursor    string                  `json:"nextCursor"`
	FilteredCount int64                   `json:"filteredCount"`
	Stats         homeservice.RecentStats `json:"stats"`
}

func loadRecentTestPage(t *testing.T, server *testServer, query string) recentTestPage {
	t.Helper()
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/v1/recent-games?"+query, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("recent page: %d %s", response.Code, response.Body.String())
	}
	var page recentTestPage
	mustDecodeHTTPTest(t, response.Body.Bytes(), &page)
	return page
}

func TestRecentPagesPreserveFullHistoryAcrossAllSorts(t *testing.T) {
	server := newTestServer(t)
	now := server.now().UnixMilli()
	seedRecentGameHistory(t, server.database, now, 75)
	for _, sort := range []string{"RECENT_DESC", "TITLE_ASC", "DURATION_DESC", "SESSIONS_DESC"} {
		t.Run(sort, func(t *testing.T) { assertRecentSortPages(t, server, sort) })
	}
	filtered := loadRecentTestPage(t, server, "q=fixture+74&limit=7")
	if len(filtered.Items) != 1 || filtered.FilteredCount != 1 || filtered.Stats.GameCount != 75 {
		t.Fatalf("server filter and global stats: %#v", filtered)
	}
	boundary := loadRecentTestPage(t, server, fmt.Sprintf("fromAtMs=%d", now-1000))
	if len(boundary.Items) != 1 {
		t.Fatalf("inclusive cutoff: %#v", boundary)
	}
	assertRecentCursorBindings(t, server)
}

func assertRecentCursorBindings(t *testing.T, server *testServer) {
	t.Helper()
	first := loadRecentTestPage(t, server, "limit=1")
	for _, query := range []string{"sort=TITLE_ASC", "q=fixture", "platformId=nes", "fromAtMs=1"} {
		assertRecentInvalidCursor(t, server, query+"&cursor="+url.QueryEscape(first.NextCursor))
	}
	server.accountDeps.Authenticator = fixedAuthenticator{Principal: authn.Principal{
		UserID: "01980000-0000-7000-8000-000000000444", ProfileID: "other", Role: "ADMIN", SessionID: "other",
	}}
	assertRecentInvalidCursor(t, server, "cursor="+url.QueryEscape(first.NextCursor))
	other := loadRecentTestPage(t, server, "limit=1")
	if len(other.Items) != 0 || other.Stats.GameCount != 0 {
		t.Fatalf("profile leak: %#v", other)
	}
}

func assertRecentInvalidCursor(t *testing.T, server *testServer, query string) {
	t.Helper()
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/v1/recent-games?"+query, nil))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "INVALID_CURSOR") {
		t.Fatalf("cursor binding: %d %s", response.Code, response.Body.String())
	}
}

func assertRecentSortPages(t *testing.T, server *testServer, sort string) {
	t.Helper()

	query := "limit=7&sort=" + sort
	seen := make(map[string]bool)
	for pageIndex := 0; ; pageIndex++ {
		page := loadRecentTestPage(t, server, query)
		if len(page.Items) > 7 {
			t.Fatal("unbounded recent page")
		}
		if pageIndex == 0 && (page.FilteredCount != 75 || page.Stats.GameCount != 75 || page.Stats.SessionCount != 75 || page.Stats.ActiveDurationMS != 75*60_000) {
			t.Fatalf("full history stats: %#v", page)
		}
		for _, item := range page.Items {
			if seen[item.GameID] {
				t.Fatalf("repeated game %s", item.GameID)
			}
			seen[item.GameID] = true
		}
		if page.NextCursor == "" {
			break
		}
		if pageIndex > 11 {
			t.Fatal("cursor did not terminate")
		}
		query = "limit=7&sort=" + sort + "&cursor=" + url.QueryEscape(page.NextCursor)
	}
	if len(seen) != 75 {
		t.Fatalf("reachable games=%d", len(seen))
	}
}
