package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestGameListRecentCursorKeepsUnplayedGamesAfterPlayedGames(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	seedRecentGameHistory(t, server.database, server.now().UnixMilli(), 4)
	mustExecHTTPTest(t, server.database, `DELETE FROM profile_game_activity
WHERE game_id IN (SELECT id FROM games WHERE title IN ('Recent fixture 02','Recent fixture 03'))`)
	cursor := ""
	for index, suffix := range []string{"00", "01", "03", "02"} {
		path := "/api/v1/games?sort=RECENT_DESC&limit=1&q=recent+fixture"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("page %d: %d %s", index, response.Code, response.Body.String())
		}
		var page struct {
			Items []struct {
				Title        string `json:"title"`
				LastPlayedMS *int64 `json:"lastPlayedAtMs"`
			} `json:"items"`
			NextCursor *string `json:"nextCursor"`
		}
		mustDecodeHTTPTest(t, response.Body.Bytes(), &page)
		if len(page.Items) != 1 || page.Items[0].Title != "Recent fixture "+suffix {
			t.Fatalf("page %d: %#v", index, page)
		}
		if (page.Items[0].LastPlayedMS == nil) != (index >= 2) {
			t.Fatalf("page %d has incorrect played state", index)
		}
		if index == 3 {
			if page.NextCursor != nil {
				t.Fatal("last page retained a cursor")
			}
			return
		}
		if page.NextCursor == nil {
			t.Fatalf("page %d lost remaining games", index)
		}
		cursor = *page.NextCursor
	}
}
