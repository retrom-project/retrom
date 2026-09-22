package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"retrom/internal/config"
)

const initialAdminBody = `{"username":"admin","displayName":"Administrator","password":"A1!x2z","passwordConfirmation":"A1!x2z"}`

func initializeAdminRequest(t *testing.T, handler http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/initialize", strings.NewReader(initialAdminBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://localhost:3000")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestAuthHTTPConcurrentInitializationCreatesExactlyOneAdministrator(t *testing.T) {
	t.Parallel()
	server := newAuthHTTPServer(t, config.ModeRelease)
	handler := server.Handler()
	assertInitializationRows(t, server, 0)
	contextResponse := httptest.NewRecorder()
	handler.ServeHTTP(contextResponse, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/context", nil))
	if contextResponse.Code != http.StatusOK || !strings.Contains(contextResponse.Body.String(), `"instanceState":"INITIALIZATION_REQUIRED"`) {
		t.Fatalf("empty release context = %d %s", contextResponse.Code, contextResponse.Body.String())
	}
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			<-start
			results <- initializeAdminRequest(t, handler)
		}()
	}
	close(start)
	counts := make(map[int]int)
	var initialized *httptest.ResponseRecorder
	for range 2 {
		result := <-results
		counts[result.Code]++
		if result.Code == http.StatusCreated {
			initialized = result
		}
	}
	if counts[http.StatusCreated] != 1 || counts[http.StatusConflict] != 1 {
		t.Fatalf("concurrent initialization statuses = %v", counts)
	}
	assertInitializationRows(t, server, 1)
	assertInitialAdministratorSession(t, handler, initialized)
	repeated := initializeAdminRequest(t, handler)
	if repeated.Code != http.StatusConflict || !strings.Contains(repeated.Body.String(), "INITIALIZATION_ALREADY_COMPLETED") {
		t.Fatalf("repeated initialization = %d %s", repeated.Code, repeated.Body.String())
	}
	assertInitializationRows(t, server, 1)
}

func assertInitialAdministratorSession(t *testing.T, handler http.Handler, initialized *httptest.ResponseRecorder) {
	t.Helper()
	cookies := initialized.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("initialization did not issue the protected session cookie")
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/context", nil)
	request.AddCookie(cookies[0])
	authenticated := httptest.NewRecorder()
	handler.ServeHTTP(authenticated, request)
	if authenticated.Code != http.StatusOK || !strings.Contains(authenticated.Body.String(), `"authenticationState":"AUTHENTICATED"`) || !strings.Contains(authenticated.Body.String(), `"role":"ADMIN"`) {
		t.Fatal("initial administrator session was not authenticated")
	}
}

func assertInitializationRows(t *testing.T, server *Server, expected int) {
	t.Helper()
	var users, admins, profiles, credentials, sessions, audits int
	err := server.database.QueryRowContext(t.Context(), `
SELECT (SELECT count(*) FROM users),
 (SELECT count(*) FROM users WHERE role='ADMIN' AND status='ENABLED'),
 (SELECT count(*) FROM profiles),
 (SELECT count(*) FROM user_credentials WHERE password_scheme='ARGON2ID_V1' AND password_hash LIKE '$argon2id$%'),
 (SELECT count(*) FROM auth_sessions),
 (SELECT count(*) FROM audit_events WHERE action='INSTANCE_INITIALIZED')
`).Scan(&users, &admins, &profiles, &credentials, &sessions, &audits)
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{users, admins, profiles, credentials, sessions, audits} {
		if count != expected {
			t.Fatalf("initialization rows = %d/%d/%d/%d/%d/%d, want %d each", users, admins, profiles, credentials, sessions, audits, expected)
		}
	}
}
