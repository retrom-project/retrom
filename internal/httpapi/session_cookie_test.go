package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionCookieLastsSevenDays(t *testing.T) {
	t.Parallel()
	for _, origin := range []struct {
		url    string
		secure bool
	}{{"https://retrom.example", true}, {"http://localhost:3000", false}} {
		t.Run(origin.url, func(t *testing.T) {
			t.Parallel()
			s := &Server{CookieName: "retrom_session", Origin: origin.url}
			w := httptest.NewRecorder()
			s.setCookie(w, "test-session")
			response := w.Result()
			defer func() { _ = response.Body.Close() }()
			cookies := response.Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookies=%d", len(cookies))
			}
			cookie := cookies[0]
			if cookie.Name != "retrom_session" || cookie.Value != "test-session" || cookie.MaxAge != 7*24*60*60 {
				t.Fatalf("unexpected session cookie: %+v", cookie)
			}
			if cookie.Path != "/" || !cookie.HttpOnly || cookie.Secure != origin.secure || cookie.SameSite != http.SameSiteStrictMode {
				t.Fatalf("unexpected cookie protections: %+v", cookie)
			}
		})
	}
}

func TestLogoutCookieIsExpiredImmediately(t *testing.T) {
	t.Parallel()
	s := &Server{CookieName: "retrom_session", Origin: "https://retrom.example"}
	w := httptest.NewRecorder()
	s.setCookie(w, "")
	response := w.Result()
	defer func() { _ = response.Body.Close() }()
	cookies := response.Cookies()
	if len(cookies) != 1 || cookies[0].Value != "" || cookies[0].MaxAge != -1 || !cookies[0].Expires.Equal(time.Unix(0, 0)) {
		t.Fatalf("logout cookies=%+v", cookies)
	}
}
