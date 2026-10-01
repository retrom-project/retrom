package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	gamelistservice "retrom/internal/service/gamelist"
)

func TestAdminPagesKeepGlobalSummaryAndApplyRuntimeFilters(t *testing.T) {
	server := newTestServer(t)
	seedRecentGameHistory(t, server.database, server.now().UnixMilli(), 25)
	first := loadAdminTestPage(t, server, "limit=6")
	assertAdminInitialPage(t, first)
	second := loadAdminTestPage(t, server, "limit=6&cursor="+url.QueryEscape(first.NextCursor))
	if len(second.Items) != 6 || second.Items[0].Title != "Recent fixture 18" {
		t.Fatalf("admin next page: %#v", second)
	}
	gameID := first.Items[0].GameID
	mustExecHTTPTest(t, server.database, "UPDATE game_variants SET status='BLOCKED',emulator_game_id=NULL WHERE game_id=?", gameID)
	attention := loadAdminTestPage(t, server, "runtime=ATTENTION&limit=6")
	if len(attention.Items) != 1 || attention.Items[0].GameID != gameID || attention.FilteredCount != 1 || attention.Summary.Total != 25 || attention.Summary.RuntimeAttention != 1 {
		t.Fatalf("runtime filter: %#v", attention)
	}
	coreSearch := loadAdminTestPage(t, server, "q=dosbox&limit=6")
	if coreSearch.FilteredCount != 25 {
		t.Fatalf("default core search: %#v", coreSearch)
	}
	invalid := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalid, httptest.NewRequestWithContext(t.Context(), http.MethodGet,
		"/api/v1/admin/games?runtime=ATTENTION&cursor="+url.QueryEscape(first.NextCursor), nil))
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "INVALID_CURSOR") {
		t.Fatalf("runtime cursor binding: %d %s", invalid.Code, invalid.Body.String())
	}
}

type adminTestPage struct {
	Items []struct {
		GameID string `json:"gameId"`
		Title  string `json:"title"`
	} `json:"items"`
	NextCursor    string                       `json:"nextCursor"`
	FilteredCount int64                        `json:"filteredCount"`
	Summary       gamelistservice.AdminSummary `json:"summary"`
}

func loadAdminTestPage(t *testing.T, server *testServer, query string) adminTestPage {
	t.Helper()
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/games?"+query, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("admin page: %d %s", response.Code, response.Body.String())
	}
	var result adminTestPage
	mustDecodeHTTPTest(t, response.Body.Bytes(), &result)
	return result
}

func assertAdminInitialPage(t *testing.T, first adminTestPage) {
	t.Helper()
	if len(first.Items) != 6 || first.NextCursor == "" || first.FilteredCount != 25 || first.Summary.Total != 25 || first.Summary.MissingCover != 25 {
		t.Fatalf("first admin page: %#v", first)
	}
}
