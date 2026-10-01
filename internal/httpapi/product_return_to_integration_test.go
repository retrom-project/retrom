//go:build integration

package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"retrom/internal/authn"
	dbapi "retrom/internal/database"
)

func TestProductCreateHTTPRecentReturnPage(t *testing.T) {
	for _, path := range []string{"/recent", "/recent?redirect=https://example.invalid"} {
		t.Run(path, func(t *testing.T) {
			server := newReadyHTTPServer(t)
			gameID, _ := seedMovableGame(t, server)
			ctx := authn.WithPrincipal(t.Context(), authn.Principal{UserID: "01980000-0000-7000-8000-000000009999", ProfileID: "local", SessionID: "01980000-0000-7000-8000-000000009998"})
			body := fmt.Sprintf(`{"gameId":%q,"returnTo":%q,"clientCapabilities":{"secureContext":true,"crossOriginIsolated":true,"sharedArrayBuffer":true}}`, gameID, path)
			response := productCreateHTTPBody(ctx, server, body)
			launches, receipts := productCreateHTTPCounts(t, server)
			if path != "/recent" {
				if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "INVALID_LAUNCH_RETURN_TO") || launches != 0 || receipts != 0 {
					t.Fatalf("status=%d launches=%d receipts=%d", response.Code, launches, receipts)
				}
				return
			}
			if response.Code != http.StatusCreated || launches != 1 || receipts != 1 {
				t.Fatalf("status=%d launches=%d receipts=%d", response.Code, launches, receipts)
			}
			var returned string
			if err := dbapi.QueryRowContext(t.Context(), server.database, `SELECT return_to FROM launch_sessions`).Scan(&returned); err != nil || returned != path {
				t.Fatalf("return page=%q error=%v", returned, err)
			}
		})
	}
}
