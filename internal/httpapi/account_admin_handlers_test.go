package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/config"
	"retrom/internal/testassert"
)

type accountHTTPAuth struct {
	cookie *http.Cookie
	csrf   string
}

func accountHTTPLogin(t *testing.T, handler http.Handler) accountHTTPAuth {
	t.Helper()
	request := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"username":"test","password":"test"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://localhost:3000")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var body struct {
		CSRF string `json:"csrfToken"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	cookies := recorder.Result().Cookies()
	testassert.Falsef(t, testassert.Any(func() bool { return recorder.Code != http.StatusOK }, func() bool { return len(cookies) != 1 }, func() bool { return body.CSRF == "" }), "login = %d cookies=%#v body=%s", recorder.Code, cookies, recorder.Body.String())
	return accountHTTPAuth{cookie: cookies[0], csrf: body.CSRF}
}

func accountHTTPRequest(
	t *testing.T,
	handler http.Handler,
	method, target, body string,
	auth *accountHTTPAuth,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead {
		request.Header.Set("Origin", "http://localhost:3000")
	}
	if auth != nil {
		request.AddCookie(auth.cookie)
		request.Header.Set("X-Retrom-Csrf", auth.csrf)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestAccountAdministrationHTTPInvitationAndAuthorization(t *testing.T) {
	t.Parallel()
	server, _ := newAuthHTTPServer(t, config.ModeTest)
	handler := server.Handler()
	admin := accountHTTPLogin(t, handler)

	createRequest := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/v1/admin/invitations", strings.NewReader(`{"role":"USER","confirmAdminRole":false}`),
	)
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set("Origin", "http://localhost:3000")
	createRequest.Header.Set("X-Retrom-Csrf", admin.csrf)
	createRequest.Header.Set("Idempotency-Key", uuid.NewString())
	createRequest.AddCookie(admin.cookie)
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, createRequest)
	var invitation struct {
		AccountLinkID string `json:"accountLinkId"`
		URL           string `json:"url"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &invitation); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return created.Code != http.StatusCreated }, func() bool { return invitation.AccountLinkID == "" }, func() bool { return invitation.URL == "" }), "create invitation = %d %s", created.Code, created.Body.String())
	linkURL, err := url.Parse(invitation.URL)
	testassert.False(t, err != nil, err)
	token, err := url.QueryUnescape(strings.TrimPrefix(linkURL.Fragment, "invite="))
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return token == "" }), "invitation token = %q, %v", token, err)

	inspection := accountHTTPRequest(
		t, handler, http.MethodPost, "/api/v1/auth/account-links/inspect",
		`{"expectedKind":"INVITATION","token":"`+token+`"}`, nil,
	)
	testassert.Falsef(t, testassert.Any(func() bool { return inspection.Code != http.StatusOK }, func() bool { return !strings.Contains(inspection.Body.String(), `"role":"USER"`) }), "inspect invitation = %d %s", inspection.Code, inspection.Body.String())

	accepted := accountHTTPRequest(
		t, handler, http.MethodPost, "/api/v1/auth/invitations/accept",
		`{"token":"`+token+`","username":"alice","displayName":"Alice","password":"a sufficiently long passphrase","passwordConfirmation":"a sufficiently long passphrase"}`,
		nil,
	)
	var acceptedBody struct {
		CSRF                     string `json:"csrfToken"`
		TestDefaultAccountActive bool   `json:"testDefaultAccountActive"`
	}
	if err := json.Unmarshal(accepted.Body.Bytes(), &acceptedBody); err != nil {
		t.Fatal(err)
	}
	acceptedCookies := accepted.Result().Cookies()
	testassert.Falsef(t, testassert.Any(func() bool { return accepted.Code != http.StatusCreated }, func() bool { return len(acceptedCookies) != 1 }, func() bool { return acceptedBody.CSRF == "" }, func() bool { return !acceptedBody.TestDefaultAccountActive }), "accept invitation = %d cookies=%#v body=%s", accepted.Code, acceptedCookies, accepted.Body.String())
	member := accountHTTPAuth{cookie: acceptedCookies[0], csrf: acceptedBody.CSRF}

	users := accountHTTPRequest(t, handler, http.MethodGet, "/api/v1/admin/users?q=alice", "", &admin)
	testassert.Falsef(t, testassert.Any(func() bool { return users.Code != http.StatusOK }, func() bool { return !strings.Contains(users.Body.String(), `"username":"alice"`) }, func() bool { return strings.Contains(users.Body.String(), "passwordHash") }), "list users = %d %s", users.Code, users.Body.String())
	emptyQuery := accountHTTPRequest(t, handler, http.MethodGet, "/api/v1/admin/users?q=%20", "", &admin)
	testassert.Falsef(t, testassert.Any(func() bool { return emptyQuery.Code != http.StatusBadRequest }, func() bool { return !strings.Contains(emptyQuery.Body.String(), "INVALID_QUERY") }), "empty user query = %d %s", emptyQuery.Code, emptyQuery.Body.String())

	memberCreate := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/v1/admin/invitations", strings.NewReader(`{"role":"USER","confirmAdminRole":false}`),
	)
	memberCreate.Header.Set("Content-Type", "application/json")
	memberCreate.Header.Set("Origin", "http://localhost:3000")
	memberCreate.Header.Set("X-Retrom-Csrf", member.csrf)
	memberCreate.Header.Set("Idempotency-Key", uuid.NewString())
	memberCreate.AddCookie(member.cookie)
	forbidden := httptest.NewRecorder()
	handler.ServeHTTP(forbidden, memberCreate)
	testassert.Falsef(t, testassert.Any(func() bool { return forbidden.Code != http.StatusForbidden }, func() bool { return !strings.Contains(forbidden.Body.String(), "ADMIN_REQUIRED") }), "member admin operation = %d %s", forbidden.Code, forbidden.Body.String())
}
