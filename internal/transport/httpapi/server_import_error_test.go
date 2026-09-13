package httpapi

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"retrom/internal/service/serverimport"
)

func TestServerImportErrorUsesDomainMissingRecord(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{{"missing import", serverimport.ErrNotFound, http.StatusNotFound}, {"missing required storage row", fmt.Errorf("read required job: %w", sql.ErrNoRows), http.StatusInternalServerError}} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/server-imports/missing", nil)
			response := httptest.NewRecorder()
			(&Server{}).writeServerImportError(response, request, test.err)
			if response.Code != test.status {
				t.Fatalf("error status=%d want=%d body=%s", response.Code, test.status, response.Body.String())
			}
		})
	}
}
