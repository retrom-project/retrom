package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"retrom/internal/authn"
	"retrom/internal/config"

	"github.com/google/uuid"
)

func TestReviewDeduplicateHTTPContractAndReplay(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()
	cookie, csrf := testSessionCredentials()
	key := uuid.NewString()
	send := func(body, token, idempotency string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
			"/api/v1/admin/reviews/deduplicate", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", idempotency)
		setCSRFCredentials(request, cookie, token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	first := send(`{"scope":{}}`, csrf, key)
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"discardedCount":0`) {
		t.Fatalf("empty = %d %s", first.Code, first.Body.String())
	}
	replay := send(`{"scope":{}}`, csrf, key)
	if replay.Code != http.StatusOK || replay.Body.String() != first.Body.String() {
		t.Fatalf("replay = %d %s", replay.Code, replay.Body.String())
	}
	for _, body := range []string{`{}`, `{"scope":{"unknown":true}}`, `{"scope":{},"afterItemId":"bad"}`} {
		response := send(body, csrf, uuid.NewString())
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid request = %d %s", response.Code, response.Body.String())
		}
	}
	originalMode := server.config.Mode
	server.config.Mode = config.ModeTest
	if response := send(`{"scope":{}}`, "wrong", uuid.NewString()); response.Code != http.StatusForbidden {
		t.Fatalf("CSRF status = %d", response.Code)
	}
	server.config.Mode = originalMode
	if response := send(`{"scope":{}}`, csrf, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("missing key status = %d", response.Code)
	}
	server.authenticator = fixedAuthenticator{Principal: authn.Principal{UserID: "01980000-0000-7000-8000-000000009999", Role: "USER"}}
	if response := send(`{"scope":{}}`, csrf, uuid.NewString()); response.Code != http.StatusForbidden {
		t.Fatalf("non-admin status = %d", response.Code)
	}
}
