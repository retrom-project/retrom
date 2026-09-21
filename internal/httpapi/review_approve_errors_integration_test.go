//go:build integration

package httpapi

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"retrom/internal/composition"
	"retrom/internal/testsupport"
)

func TestReviewApproveSQLFailuresAreServerErrors(t *testing.T) {
	t.Parallel()
	for _, match := range []string{"JOIN review_drafts d", "INSERT INTO review_events"} {
		t.Run(match, func(t *testing.T) {
			t.Parallel()
			server := newTestServer(t)
			itemID := createReviewSnapshotItem(t, server)
			cause := errors.New("approval store unavailable")
			hits := 0
			database := testsupport.OpenSQLFaultDatabase(t, server.database, testsupport.SQLFaultHooks{
				BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
					if strings.Contains(query, match) {
						hits++
						return cause
					}
					return nil
				},
			})
			server.reviewApprovals = composition.NewLibraryReviewApprovals(database, server.now)
			response := requestReviewApprove(t, server, itemID, `"v1"`, `{}`)
			if response.Code != http.StatusInternalServerError || hits != 1 {
				t.Fatalf("approval SQL failure became conflict: status=%d hits=%d body=%s", response.Code, hits, response.Body.String())
			}
		})
	}
}

func requestReviewApprove(t *testing.T, server *Server, itemID, version, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/reviews/"+itemID+"/approve", strings.NewReader(body))
	request.SetPathValue("importItemId", itemID)
	request.Header.Set("If-Match", version)
	request.Header.Set("Idempotency-Key", "01980000-0000-7000-8000-000000009601")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.approveReview(response, request)
	return response
}
