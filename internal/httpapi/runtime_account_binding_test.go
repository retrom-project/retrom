package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	runtimepersistence "retrom/internal/persistence/runtimesession"

	"retrom/internal/authn"
	"retrom/internal/config"
	"retrom/internal/service/runtimesession"
)

func TestAccountReauthenticationPreservesCurrentRuntimeButSwitchRevokesIt(t *testing.T) {
	t.Parallel()
	server := newAuthHTTPServer(t, config.ModeTest)
	account, err := server.accountDeps.Accounts.Login(t.Context(), "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	issued, err := server.playDeps.RuntimeSessions.Ensure(t.Context(), account.Principal.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: runtimeCookieName, Value: issued.Token}
	request := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v1/launches", nil)
	request.AddCookie(cookie)
	sameUser := account.Principal
	sameUser.SessionID = "another-auth-session"
	reused, err := server.runtimeSessionForPrincipal(request, sameUser)
	if err != nil || reused.Token != cookie.Value {
		t.Fatal("reauthentication must not rotate a still-valid runtime token")
	}
	response := httptest.NewRecorder()
	if !server.clearRuntimeAfterAccountSwitch(response, request, sameUser) || len(response.Result().Cookies()) != 0 {
		t.Fatal("same account reauthentication cleared the runtime cookie")
	}
	response = httptest.NewRecorder()
	if !server.clearRuntimeAfterAccountSwitch(response, request, authn.Principal{ProfileID: "different-user"}) {
		t.Fatal("account switch failed")
	}
	if _, err = server.playDeps.RuntimeSessions.Authenticate(t.Context(), cookie.Value); !errors.Is(err, runtimesession.ErrCredential) {
		t.Fatal("account switch retained the previous account runtime authority")
	}
	cleared := response.Result().Cookies()
	if len(cleared) != 1 || cleared[0].Name != runtimeCookieName || cleared[0].MaxAge != -1 {
		t.Fatal("account switch did not clear shared cookie")
	}
}

func TestAccountReauthenticationRefreshesBrowserRuntimeExpiry(t *testing.T) {
	t.Parallel()
	server := newAuthHTTPServer(t, config.ModeTest)
	now := server.now()
	server.now = func() time.Time { return now }
	server.playDeps.RuntimeSessions = runtimesession.New(runtimepersistence.New(server.database), runtimesession.Environment{
		Now: server.now, Sign: server.contentDeps.Credentials.RuntimeSession,
		NewID: func() (string, error) { id, err := uuid.NewV7(); return id.String(), err },
	})
	account, err := server.accountDeps.Accounts.Login(t.Context(), "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	issued, err := server.playDeps.RuntimeSessions.Ensure(t.Context(), account.Principal.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(13 * time.Hour)
	request := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v1/auth/login", nil)
	request.AddCookie(&http.Cookie{Name: runtimeCookieName, Value: issued.Token})
	response := httptest.NewRecorder()
	if !server.clearRuntimeAfterAccountSwitch(response, request, account.Principal) {
		t.Fatal("reauthentication failed")
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value != issued.Token || cookies[0].Expires.Unix() != now.Add(24*time.Hour).Unix() {
		t.Fatal("reauthentication renewed server lease without synchronizing browser expiry")
	}
	refreshed, err := server.playDeps.RuntimeSessions.Authenticate(t.Context(), issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	request = request.WithContext(context.WithValue(request.Context(), runtimeSessionKey{}, refreshed))
	server.renewRuntimeSession(response, request)
	cookies = response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value != issued.Token || cookies[0].Expires.Unix() != now.Add(24*time.Hour).Unix() {
		t.Fatal("periodic renewal did not synchronize an already-renewed browser credential")
	}
}
