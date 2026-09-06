//go:build integration

package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"retrom/internal/config"
)

func TestLocalGameSaveRequiresAccountAndCSRF(t *testing.T) {
	server := newTestServer(t)
	server.config.Mode = config.ModeTest
	cookie, _ := testSessionCredentials()
	for _, authenticated := range []bool{false, true} {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
			"/api/v1/launches/01980000-0000-7000-8000-000000000001/local-save", strings.NewReader(""))
		request.Header.Set("Content-Type", "multipart/form-data; boundary=test")
		request.Header.Set("Idempotency-Key", "01980000-0000-7000-8000-000000000099")
		request.Header.Set("Origin", server.config.PublicOrigin.String())
		server.authenticator = nil
		if authenticated {
			server.authenticator = testAuthenticator{}
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		expected := http.StatusUnauthorized
		if authenticated {
			expected = http.StatusForbidden
		}
		if response.Code != expected {
			t.Fatalf("authenticated=%v: %d %s", authenticated, response.Code, response.Body.String())
		}
		if authenticated && !strings.Contains(response.Body.String(), "CSRF_VALIDATION_FAILED") {
			t.Fatalf("missing CSRF validation: %s", response.Body.String())
		}
	}
}
