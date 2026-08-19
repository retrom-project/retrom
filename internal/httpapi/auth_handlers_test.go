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
)

func newAuthHTTPServer(t *testing.T, mode config.Mode) (*Server, *retromruntime.Credentials) {
	t.Helper()
	root := t.TempDir()
	now := func() time.Time { return time.UnixMilli(1_786_000_000_000).UTC() }
	database, err := store.Open(context.Background(), filepath.Join(root, "retrom.db"), now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	credentials, err := retromruntime.LoadOrCreateCredentials(root)
	if err != nil {
		t.Fatal(err)
	}
	accountService, err := accounts.New(
		context.Background(), database.SQL, credentials, mode, authn.EmptyBlocklist{}, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := accountService.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	repositoryRoot, _ := filepath.Abs(filepath.Join("..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
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
	handler.ServeHTTP(contextRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/auth/context", nil))
	if contextRecorder.Code != http.StatusOK ||
		!strings.Contains(contextRecorder.Body.String(), `"authenticationState":"UNAUTHENTICATED"`) ||
		!strings.Contains(contextRecorder.Body.String(), `"testDefaultAccountActive":true`) {
		t.Fatalf("context = %d %s", contextRecorder.Code, contextRecorder.Body.String())
	}

	loginRequest := httptest.NewRequest(
		http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"test","password":"test"}`),
	)
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRequest.Header.Set("Origin", "http://localhost:3000")
	loginRequest.Header.Set("Sec-Fetch-Site", "same-origin")
	loginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(loginRecorder, loginRequest)
	cookies := loginRecorder.Result().Cookies()
	if loginRecorder.Code != http.StatusOK || len(cookies) != 1 || cookies[0].Name != "retrom_session" ||
		!cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Secure {
		t.Fatalf("login = %d cookies=%#v body=%s", loginRecorder.Code, cookies, loginRecorder.Body.String())
	}
	var csrfToken string
	for _, fragment := range strings.Split(loginRecorder.Body.String(), `"`) {
		if len(fragment) == 43 {
			csrfToken = fragment
		}
	}
	if csrfToken == "" {
		t.Fatalf("login csrf missing: %s", loginRecorder.Body.String())
	}

	protected := httptest.NewRequest(http.MethodGet, "/api/v1/home", nil)
	protectedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(protectedRecorder, protected)
	if protectedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous home = %d %s", protectedRecorder.Code, protectedRecorder.Body.String())
	}

	change := httptest.NewRequest(
		http.MethodPost, "/api/v1/auth/change-password",
		strings.NewReader(`{"currentPassword":"test","newPassword":"a sufficiently long new phrase","newPasswordConfirmation":"a sufficiently long new phrase"}`),
	)
	change.Header.Set("Content-Type", "application/json")
	change.Header.Set("Origin", "http://localhost:3000")
	change.AddCookie(cookies[0])
	changeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(changeRecorder, change)
	if changeRecorder.Code != http.StatusForbidden || !strings.Contains(changeRecorder.Body.String(), "CSRF_VALIDATION_FAILED") {
		t.Fatalf("missing csrf = %d %s", changeRecorder.Code, changeRecorder.Body.String())
	}

	logout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", strings.NewReader(`{}`))
	logout.Header.Set("Content-Type", "application/json")
	logout.Header.Set("Origin", "http://localhost:3000")
	logout.Header.Set("X-Retrom-Csrf", csrfToken)
	logout.AddCookie(cookies[0])
	logoutRecorder := httptest.NewRecorder()
	handler.ServeHTTP(logoutRecorder, logout)
	if logoutRecorder.Code != http.StatusNoContent || logoutRecorder.Header().Get("Clear-Site-Data") == "" {
		t.Fatalf("logout = %d %s", logoutRecorder.Code, logoutRecorder.Body.String())
	}
}

func TestAuthHTTPReleasePendingRequiresSetupAndExactOrigin(t *testing.T) {
	t.Parallel()
	server, credentials := newAuthHTTPServer(t, config.ModeRelease)
	handler := server.Handler()
	ordinary := httptest.NewRecorder()
	handler.ServeHTTP(ordinary, httptest.NewRequest(http.MethodGet, "/api/v1/home", nil))
	if ordinary.Code != http.StatusPreconditionRequired || !strings.Contains(ordinary.Body.String(), "INITIALIZATION_REQUIRED") {
		t.Fatalf("pending ordinary = %d %s", ordinary.Code, ordinary.Body.String())
	}
	body := `{"setupCode":"` + credentials.SetupCode() + `","username":"admin","displayName":"Administrator","password":"A1!x2z","passwordConfirmation":"A1!x2z"}`
	crossSite := httptest.NewRequest(http.MethodPost, "/api/v1/auth/initialize", strings.NewReader(body))
	crossSite.Header.Set("Content-Type", "application/json")
	crossSite.Header.Set("Origin", "http://attacker.invalid")
	crossRecorder := httptest.NewRecorder()
	handler.ServeHTTP(crossRecorder, crossSite)
	if crossRecorder.Code != http.StatusForbidden || !strings.Contains(crossRecorder.Body.String(), "REQUEST_ORIGIN_INVALID") {
		t.Fatalf("cross-site setup = %d %s", crossRecorder.Code, crossRecorder.Body.String())
	}
	tooShortBody := `{"setupCode":"` + credentials.SetupCode() + `","username":"admin","displayName":"Administrator","password":"A1!x2","passwordConfirmation":"A1!x2"}`
	tooShort := httptest.NewRequest(http.MethodPost, "/api/v1/auth/initialize", strings.NewReader(tooShortBody))
	tooShort.Header.Set("Content-Type", "application/json")
	tooShort.Header.Set("Origin", "http://localhost:3000")
	tooShortRecorder := httptest.NewRecorder()
	handler.ServeHTTP(tooShortRecorder, tooShort)
	if tooShortRecorder.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(tooShortRecorder.Body.String(), `"code":"PASSWORD_POLICY_VIOLATION"`) ||
		!strings.Contains(tooShortRecorder.Body.String(), `"reasonCode":"TOO_SHORT"`) {
		t.Fatalf("five-character setup = %d %s", tooShortRecorder.Code, tooShortRecorder.Body.String())
	}
	initialize := httptest.NewRequest(http.MethodPost, "/api/v1/auth/initialize", strings.NewReader(body))
	initialize.Header.Set("Content-Type", "application/json")
	initialize.Header.Set("Origin", "http://localhost:3000")
	initializeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(initializeRecorder, initialize)
	if initializeRecorder.Code != http.StatusCreated || len(initializeRecorder.Result().Cookies()) != 1 {
		t.Fatalf("initialize = %d %s", initializeRecorder.Code, initializeRecorder.Body.String())
	}
}

func TestAuthHTTPLoginRateLimitReturnsRetryAfter(t *testing.T) {
	t.Parallel()
	server, _ := newAuthHTTPServer(t, config.ModeTest)
	handler := server.Handler()
	for attempt := 1; attempt <= 5; attempt++ {
		request := httptest.NewRequest(
			http.MethodPost, "/api/v1/auth/login",
			strings.NewReader(`{"username":"test","password":"wrong"}`),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://localhost:3000")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if attempt < 5 && recorder.Code != http.StatusUnauthorized {
			t.Fatalf("login failure %d = %d %s", attempt, recorder.Code, recorder.Body.String())
		}
		if attempt == 5 && (recorder.Code != http.StatusTooManyRequests ||
			recorder.Header().Get("Retry-After") != "900" ||
			!strings.Contains(recorder.Body.String(), "AUTH_RATE_LIMITED")) {
			t.Fatalf("login threshold = %d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
		}
	}
}
