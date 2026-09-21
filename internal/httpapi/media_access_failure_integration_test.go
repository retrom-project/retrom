//go:build integration

package httpapi

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mediarepo "retrom/internal/persistence/mediaaccess"
	"retrom/internal/service/mediaaccess"

	"retrom/internal/authn"
	"retrom/internal/testsupport"
)

func TestMediaAccessPreservesStorageFailureBoundary(t *testing.T) {
	for _, name := range []string{"game", "save", "review", "review-source"} {
		t.Run(name, func(t *testing.T) {
			now := func() time.Time { return time.Date(2028, 3, 4, 5, 6, 7, 0, time.UTC) }
			database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), now)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := database.Close(); err != nil {
					t.Error(err)
				}
			})
			cause := errors.New("media authorization database read unavailable")
			hits := 0
			table := map[string]string{
				"game": "FROM game_assets", "save": "FROM save_states",
				"review": "FROM scrape_candidate_assets", "review-source": "FROM pegasus_import_item_assets",
			}[name]
			fault := testsupport.OpenSQLFaultDatabase(t, database.SQL, testsupport.SQLFaultHooks{
				BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
					if strings.Contains(query, table) && len(args) > 0 && args[0].Value == "01980000-0000-7000-8000-000000000001" {
						hits++
						return cause
					}
					return nil
				},
			})
			server := &Server{mediaAccess: mediaaccess.New(mediarepo.New(fault))}
			handler := map[string]http.HandlerFunc{
				"game": server.contentAsset, "save": server.saveStateScreenshot,
				"review": server.reviewCandidateAsset, "review-source": server.reviewCandidateAsset,
			}[name]
			ctx := authn.WithPrincipal(t.Context(), authn.Principal{ProfileID: "local"})
			request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/media", nil)
			request.SetPathValue("assetId", "01980000-0000-7000-8000-000000000001")
			request.SetPathValue("saveStateId", "01980000-0000-7000-8000-000000000001")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusInternalServerError || hits != 1 {
				t.Fatalf("storage error became missing resource: status=%d hits=%d body=%s", response.Code, hits, response.Body.String())
			}
		})
	}
}
