//go:build integration

package httpapi

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"retrom/internal/composition"
	"retrom/internal/testsupport"
)

func TestReviewDiscardSQLFailuresAreServerErrors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, query string }{
		{"evidence read", "JOIN import_items d"}, {"item transition", "UPDATE import_items SET state="},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := newTestServer(t)
			itemID := createReviewSnapshotItem(t, server)
			cause := errors.New("review discard database unavailable")
			hits := 0
			fault := func(_ context.Context, query string, _ []driver.NamedValue) error {
				if strings.Contains(strings.Join(strings.Fields(query), " "), test.query) {
					hits++
					return cause
				}
				return nil
			}
			database := testsupport.OpenSQLFaultDatabase(t, server.database, testsupport.SQLFaultHooks{BeforeQuery: fault, BeforeExec: fault})
			server.reviewDiscards = composition.NewLibraryReviewDiscards(database, server.now)
			response := requestReviewDiscard(t, server, itemID, `"v1"`)
			if response.Code != http.StatusInternalServerError || hits != 1 {
				t.Fatalf("SQL failure became decision conflict: status=%d hits=%d body=%s", response.Code, hits, response.Body.String())
			}
		})
	}
}

func requestReviewDiscard(t *testing.T, server *Server, itemID, version string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/reviews/"+itemID+"/discard", strings.NewReader(`{"reason":"test discard"}`))
	request.SetPathValue("importItemId", itemID)
	request.Header.Set("If-Match", version)
	request.Header.Set("Idempotency-Key", "01980000-0000-7000-8000-000000008801")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.discardReview(response, request)
	return response
}

func TestReviewDiscardSuccessAndVersionConflict(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	itemID := createReviewSnapshotItem(t, server)
	response := requestReviewDiscard(t, server, itemID, `"v1"`)
	var result struct {
		ItemID      string `json:"itemId"`
		Status      string `json:"status"`
		Version     int64  `json:"version"`
		UpdatedAtMS int64  `json:"updatedAtMs"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || result.ItemID != itemID || result.Status != "DISCARDED" || result.Version != 2 || result.UpdatedAtMS <= 0 {
		t.Fatalf("discard response changed: status=%d body=%s", response.Code, response.Body.String())
	}
	repeated := requestReviewDiscard(t, server, itemID, `"v1"`)
	if repeated.Code != http.StatusConflict || !strings.Contains(repeated.Body.String(), `"REVIEW_DECISION_CONFLICT"`) {
		t.Fatalf("terminal review accepted second decision: status=%d body=%s", repeated.Code, repeated.Body.String())
	}
	var count int
	if err := server.database.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM import_items WHERE id=? AND state='DISCARDED'`, itemID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("discard replay retained %d terminal items", count)
	}
}
