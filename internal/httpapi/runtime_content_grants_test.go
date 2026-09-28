package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func testRuntimeCookie(t *testing.T, server *testServer) *http.Cookie {
	t.Helper()
	session, err := server.playDeps.RuntimeSessions.Ensure(t.Context(), "01980000-0000-7000-8000-000000009998")
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: runtimeCookieName, Value: session.Token, Path: "/"}
}

func TestSharedRuntimeCookieHasControlledDomainAndFixedName(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	server.config.PublicOrigin.Scheme = "https"
	session, err := server.playDeps.RuntimeSessions.Ensure(t.Context(), "01980000-0000-7000-8000-000000009998")
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if err := server.setRuntimeCookie(recorder, session); err != nil {
		t.Fatal(err)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count=%d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != runtimeCookieName || cookie.Path != "/" || cookie.Domain != "localhost" ||
		!cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge < 86399 {
		t.Fatal("shared runtime cookie scope or expiry is incorrect")
	}
}

func TestSharedRuntimeRejectsMissingForgedAndDuplicateCredentials(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	valid := testRuntimeCookie(t, server)
	for _, cookies := range [][]*http.Cookie{nil, {{Name: runtimeCookieName, Value: "forged"}}, {valid, valid}} {
		request := httptest.NewRequestWithContext(t.Context(), "GET", "/runtime/content/game/identity/game.zip", nil)
		for _, cookie := range cookies {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		if _, ok := server.authenticateRuntimeRequest(response, request); ok || response.Code != http.StatusUnauthorized {
			t.Fatalf("invalid shared runtime credential accepted: %d", response.Code)
		}
	}
}
