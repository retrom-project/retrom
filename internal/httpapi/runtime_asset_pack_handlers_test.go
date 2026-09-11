package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestRetiredRuntimePackRoutesAreUnavailable(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()
	cookie, csrf := testSessionCredentials()
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/admin/runtime-asset-packs"},
		{http.MethodPost, "/api/v1/admin/runtime-asset-packs/installations"},
		{http.MethodDelete, "/api/v1/admin/runtime-asset-packs/installations/01980000-0000-7000-8000-000000009996"},
	} {
		request := httptest.NewRequestWithContext(t.Context(), route.method, route.path, strings.NewReader(`{}`))
		request.AddCookie(cookie)
		request.Header.Set("Origin", "http://localhost:3000")
		request.Header.Set("X-Retrom-Csrf", csrf)
		request.Header.Set("Idempotency-Key", uuid.NewString())
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d: %s", route.method, route.path, response.Code, response.Body.String())
		}
	}
}
