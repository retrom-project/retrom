package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLaunchContentGrantCookieHasRestrictedBrowserScope(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	server.config.PublicOrigin.Scheme = "https"
	const launchID = "01980000-0000-7000-8000-000000000001"
	recorder := httptest.NewRecorder()
	server.setLaunchContentGrant(recorder, launchID, "capability", 86400)
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != runtimeContentGrantPrefix+launchID || cookie.Value != "capability" ||
		cookie.Path != "/runtime/content/" || cookie.MaxAge != 86400 || !cookie.HttpOnly ||
		!cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("content grant cookie = %#v", cookie)
	}
}

func TestLaunchProjectGrantCookieHasLaunchScopedBrowserPath(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	server.config.PublicOrigin.Scheme = "https"
	const launchID = "01980000-0000-7000-8000-000000000001"
	recorder := httptest.NewRecorder()
	server.setLaunchProjectGrant(recorder, launchID, "capability", 86400)
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != runtimeProjectPrefix+launchID || cookie.Value != "capability" ||
		cookie.Path != "/runtime/projects/"+launchID+"/" || cookie.MaxAge != 86400 ||
		!cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("project grant cookie = %#v", cookie)
	}
}

func TestRuntimeProjectGrantRequiresOneExactLaunchCookie(t *testing.T) {
	t.Parallel()
	const launchID = "01980000-0000-7000-8000-000000000001"
	request := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/runtime/projects/"+launchID+"/index.json", nil,
	)
	request.AddCookie(&http.Cookie{Name: runtimeProjectPrefix + launchID, Value: "capability"})
	grant, ok := runtimeProjectGrant(request, launchID)
	if !ok || grant.LaunchID != launchID || grant.Capability != "capability" {
		t.Fatalf("grant = %#v, ok=%t", grant, ok)
	}

	duplicate := httptest.NewRequestWithContext(context.Background(), http.MethodGet, request.URL.String(), nil)
	duplicate.Header.Add("Cookie", runtimeProjectPrefix+launchID+"=first")
	duplicate.Header.Add("Cookie", runtimeProjectPrefix+launchID+"=second")
	if grant, ok := runtimeProjectGrant(duplicate, launchID); ok || grant != (runtimeContentGrant{}) {
		t.Fatalf("duplicate grant = %#v, ok=%t", grant, ok)
	}

	other := httptest.NewRequestWithContext(context.Background(), http.MethodGet, request.URL.String(), nil)
	other.AddCookie(&http.Cookie{
		Name: runtimeProjectPrefix + "01980000-0000-7000-8000-000000000002", Value: "capability",
	})
	if _, ok := runtimeProjectGrant(other, launchID); ok {
		t.Fatal("cross-Launch project grant accepted")
	}
}

func TestRuntimeContentGrantsRejectMalformedDuplicateAndUnboundedCookies(t *testing.T) {
	t.Parallel()
	const launchID = "01980000-0000-7000-8000-000000000001"
	valid := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/runtime/content/game/identity/game.zip", nil)
	valid.AddCookie(&http.Cookie{Name: runtimeContentGrantPrefix + launchID, Value: "capability"})
	grants, ok := runtimeContentGrants(valid)
	if !ok || len(grants) != 1 || grants[0].LaunchID != launchID || grants[0].Capability != "capability" {
		t.Fatalf("valid grants = %#v, ok=%t", grants, ok)
	}

	duplicate := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/runtime/content/game/identity/game.zip", nil)
	duplicate.Header.Add("Cookie", runtimeContentGrantPrefix+launchID+"=first")
	duplicate.Header.Add("Cookie", runtimeContentGrantPrefix+launchID+"=second")
	if grants, ok := runtimeContentGrants(duplicate); ok || grants != nil {
		t.Fatalf("duplicate grants = %#v, ok=%t", grants, ok)
	}

	tooMany := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/runtime/content/game/identity/game.zip", nil)
	for index := 0; index <= maxRuntimeContentGrants; index++ {
		launchID := fmt.Sprintf("01980000-0000-7000-8000-%012d", index)
		tooMany.AddCookie(&http.Cookie{Name: runtimeContentGrantPrefix + launchID, Value: "capability"})
	}
	if grants, ok := runtimeContentGrants(tooMany); ok || grants != nil {
		t.Fatalf("unbounded grants = %#v, ok=%t", grants, ok)
	}
}
