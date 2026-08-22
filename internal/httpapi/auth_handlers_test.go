package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/accounts"
	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	"retrom/internal/dependencies"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

func newAuthHTTPServer(t *testing.T, mode config.Mode) (*Server, *retromruntime.Credentials) {
	t.Helper()
	root := t.TempDir()
	now := func() time.Time { return time.UnixMilli(1_786_000_000_000).UTC() }
	database, err := store.Open(context.Background(), filepath.Join(root, "retrom.db"), now)
	testassert.False(t, err != nil, err)
	if err := testsupport.SeedPlatformInstances(context.Background(), database.SQL); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	credentials, err := retromruntime.LoadOrCreateCredentials(root)
	testassert.False(t, err != nil, err)
	accountService, err := accounts.New(
		context.Background(), database.SQL, credentials, mode, authn.EmptyBlocklist{}, now,
	)
	testassert.False(t, err != nil, err)
	if err := accountService.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	repositoryRoot, _ := filepath.Abs(filepath.Join("..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	blobs, err := blobstore.Open(root)
	testassert.False(t, err != nil, err)
	origin, _ := url.Parse("http://localhost:3000")
	server := New(
		config.Config{
			Mode: mode, PublicOrigin: origin, ActiveEJSVersion: "4.2.3", DataDir: root,
		},
		database.SQL, dependencySet, blobs, credentials, accountService, accountService, now,
	)
	return server, credentials
}

func TestAuthHTTPTestLoginCookieCSRFAndLogout(t *testing.T) {
	t.Parallel()
	server, _ := newAuthHTTPServer(t, config.ModeTest)
	handler := server.Handler()
	contextRecorder := httptest.NewRecorder()
	handler.ServeHTTP(contextRecorder, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/auth/context", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return contextRecorder.Code != http.StatusOK }, func() bool {
		return !strings.Contains(contextRecorder.Body.String(), `"authenticationState":"UNAUTHENTICATED"`)
	}, func() bool {
		return !strings.Contains(contextRecorder.Body.String(), `"testDefaultAccountActive":true`)
	}), "context = %d %s", contextRecorder.Code, contextRecorder.Body.String())

	loginRequest := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"test","password":"test"}`),
	)
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRequest.Header.Set("Origin", "http://localhost:3000")
	loginRequest.Header.Set("Sec-Fetch-Site", "same-origin")
	loginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(loginRecorder, loginRequest)
	cookies := loginRecorder.Result().Cookies()
	testassert.Falsef(t, testassert.Any(func() bool { return loginRecorder.Code != http.StatusOK }, func() bool { return len(cookies) != 1 }, func() bool { return cookies[0].Name != "retrom_session" }, func() bool { return !cookies[0].HttpOnly }, func() bool { return cookies[0].SameSite != http.SameSiteStrictMode }, func() bool { return cookies[0].Secure }), "login = %d cookies=%#v body=%s", loginRecorder.Code, cookies, loginRecorder.Body.String())
	var csrfToken string
	for _, fragment := range strings.Split(loginRecorder.Body.String(), `"`) {
		if len(fragment) == 43 {
			csrfToken = fragment
		}
	}
	testassert.Falsef(t, csrfToken == "", "login csrf missing: %s", loginRecorder.Body.String())

	protected := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/home", nil)
	protectedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(protectedRecorder, protected)
	testassert.Falsef(t, protectedRecorder.Code != http.StatusUnauthorized, "anonymous home = %d %s", protectedRecorder.Code, protectedRecorder.Body.String())

	change := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/v1/auth/change-password",
		strings.NewReader(`{"currentPassword":"test","newPassword":"a sufficiently long new phrase","newPasswordConfirmation":"a sufficiently long new phrase"}`),
	)
	change.Header.Set("Content-Type", "application/json")
	change.Header.Set("Origin", "http://localhost:3000")
	change.AddCookie(cookies[0])
	changeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(changeRecorder, change)
	testassert.Falsef(t, testassert.Any(func() bool { return changeRecorder.Code != http.StatusForbidden }, func() bool { return !strings.Contains(changeRecorder.Body.String(), "CSRF_VALIDATION_FAILED") }), "missing csrf = %d %s", changeRecorder.Code, changeRecorder.Body.String())

	logout := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/logout", strings.NewReader(`{}`))
	logout.Header.Set("Content-Type", "application/json")
	logout.Header.Set("Origin", "http://localhost:3000")
	logout.Header.Set("X-Retrom-Csrf", csrfToken)
	logout.AddCookie(cookies[0])
	logoutRecorder := httptest.NewRecorder()
	handler.ServeHTTP(logoutRecorder, logout)
	testassert.Falsef(t, testassert.Any(func() bool { return logoutRecorder.Code != http.StatusNoContent }, func() bool { return logoutRecorder.Header().Get("Clear-Site-Data") == "" }), "logout = %d %s", logoutRecorder.Code, logoutRecorder.Body.String())
}

func TestAuthHTTPReleasePendingRequiresSetupAndExactOrigin(t *testing.T) {
	t.Parallel()
	server, credentials := newAuthHTTPServer(t, config.ModeRelease)
	handler := server.Handler()
	ordinary := httptest.NewRecorder()
	handler.ServeHTTP(ordinary, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/home", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return ordinary.Code != http.StatusPreconditionRequired }, func() bool { return !strings.Contains(ordinary.Body.String(), "INITIALIZATION_REQUIRED") }), "pending ordinary = %d %s", ordinary.Code, ordinary.Body.String())
	body := `{"setupCode":"` + credentials.SetupCode() + `","username":"admin","displayName":"Administrator","password":"A1!x2z","passwordConfirmation":"A1!x2z"}`
	crossSite := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/initialize", strings.NewReader(body))
	crossSite.Header.Set("Content-Type", "application/json")
	crossSite.Header.Set("Origin", "http://attacker.invalid")
	crossRecorder := httptest.NewRecorder()
	handler.ServeHTTP(crossRecorder, crossSite)
	testassert.Falsef(t, testassert.Any(func() bool { return crossRecorder.Code != http.StatusForbidden }, func() bool { return !strings.Contains(crossRecorder.Body.String(), "REQUEST_ORIGIN_INVALID") }), "cross-site setup = %d %s", crossRecorder.Code, crossRecorder.Body.String())
	tooShortBody := `{"setupCode":"` + credentials.SetupCode() + `","username":"admin","displayName":"Administrator","password":"A1!x2","passwordConfirmation":"A1!x2"}`
	tooShort := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/initialize", strings.NewReader(tooShortBody))
	tooShort.Header.Set("Content-Type", "application/json")
	tooShort.Header.Set("Origin", "http://localhost:3000")
	tooShortRecorder := httptest.NewRecorder()
	handler.ServeHTTP(tooShortRecorder, tooShort)
	testassert.Falsef(t, testassert.Any(func() bool { return tooShortRecorder.Code != http.StatusUnprocessableEntity }, func() bool {
		return !strings.Contains(tooShortRecorder.Body.String(), `"code":"PASSWORD_POLICY_VIOLATION"`)
	}, func() bool { return !strings.Contains(tooShortRecorder.Body.String(), `"reasonCode":"TOO_SHORT"`) }), "five-character setup = %d %s", tooShortRecorder.Code, tooShortRecorder.Body.String())
	initialize := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/initialize", strings.NewReader(body))
	initialize.Header.Set("Content-Type", "application/json")
	initialize.Header.Set("Origin", "http://localhost:3000")
	initializeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(initializeRecorder, initialize)
	testassert.Falsef(t, testassert.Any(func() bool { return initializeRecorder.Code != http.StatusCreated }, func() bool { return len(initializeRecorder.Result().Cookies()) != 1 }), "initialize = %d %s", initializeRecorder.Code, initializeRecorder.Body.String())
}

func TestAuthHTTPLoginRateLimitReturnsRetryAfter(t *testing.T) {
	t.Parallel()
	server, _ := newAuthHTTPServer(t, config.ModeTest)
	handler := server.Handler()
	for attempt := 1; attempt <= 5; attempt++ {
		request := httptest.NewRequestWithContext(context.Background(),
			http.MethodPost, "/api/v1/auth/login",
			strings.NewReader(`{"username":"test","password":"wrong"}`),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://localhost:3000")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		testassert.Falsef(t, testassert.All(func() bool { return attempt < 5 }, func() bool { return recorder.Code != http.StatusUnauthorized }), "login failure %d = %d %s", attempt, recorder.Code, recorder.Body.String())
		testassert.Falsef(t, testassert.All(func() bool { return attempt == 5 }, func() bool {
			return (recorder.Code != http.StatusTooManyRequests ||
				recorder.Header().Get("Retry-After") != "900" ||
				!strings.Contains(recorder.Body.String(), "AUTH_RATE_LIMITED"))
		}), "login threshold = %d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
}
