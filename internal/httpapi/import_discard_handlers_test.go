package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/config"
)

func TestImportBatchDiscardRequiresAdminAndCSRF(t *testing.T) {
	server, _ := newAuthHTTPServer(t, config.ModeTest)
	handler := server.Handler()
	path := "/api/v1/admin/import-batches/IMPORT/01980000-0000-7000-8000-000000000111/discard"
	anonymous := httptest.NewRecorder()
	handler.ServeHTTP(anonymous, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d", anonymous.Code)
	}
	auth := accountHTTPLogin(t, handler)
	for _, test := range []struct {
		name, body, csrf string
		want             int
	}{
		{"csrf missing", "{}", "", http.StatusForbidden},
		{"strict body", `{"all":true}`, auth.csrf, http.StatusBadRequest},
		{"unknown batch", "{}", auth.csrf, http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(test.body))
			request.AddCookie(auth.cookie)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", "http://localhost:3000")
			request.Header.Set("X-Retrom-Csrf", test.csrf)
			request.Header.Set("Idempotency-Key", uuid.NewString())
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
}
