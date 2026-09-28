package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	application "retrom/internal/service/launch"
)

type unavailableWebContent struct {
	isolatedContentBoundary
	identityUnavailable bool
}

func (fixture unavailableWebContent) ProductContent(
	context.Context, string, string, bool,
) (application.ContentRecord, bool, error) {
	return application.ContentRecord{}, false, errors.New("temporary content read failure")
}

func (fixture unavailableWebContent) PreviewProject(
	context.Context, string, string, bool,
) (application.ContentRecord, bool, error) {
	return application.ContentRecord{}, false, nil
}

func (fixture unavailableWebContent) Project(
	ctx context.Context, id string, authorize application.ConfigAuthorization,
) (application.ConfigSnapshot, bool, error) {
	if fixture.identityUnavailable {
		return application.ConfigSnapshot{}, false, errors.New("temporary identity read failure")
	}
	return fixture.isolatedContentBoundary.Project(ctx, id, authorize)
}

func TestNativeContentReadFailuresRemainRetryable(t *testing.T) {
	for _, identityUnavailable := range []bool{false, true} {
		fixture := unavailableWebContent{
			isolatedContentBoundary: isolatedContentBoundary{content: application.ContentView{
				Format: "RPG_MAKER_PROJECT", Digest: strings.Repeat("a", 64),
			}}, identityUnavailable: identityUnavailable,
		}
		now := func() time.Time { return time.UnixMilli(1000) }
		matches := func(capability string, _ []byte) bool { return capability == "valid" }
		server := &Server{playDeps: PlayDependencies{Launcher: application.New(application.ServiceDependencies{
			Content:  application.NewContentAccess(fixture, now, matches),
			Projects: application.NewProjectQueries(fixture, now, matches),
		})}}
		identity, err := application.ProjectIdentity(fixture.files())
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/runtime/content/web/test", nil)
		request.SetPathValue("contentIdentity", identity)
		request.SetPathValue("projectPath", "files/index.html")
		request = request.WithContext(context.WithValue(request.Context(), runtimeGrantsKey{},
			[]runtimeContentGrant{{LaunchID: "launch", Capability: "valid"}}))
		response := httptest.NewRecorder()
		server.launchWebContent(response, request)
		if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" {
			t.Fatalf("identity failure=%t: status=%d, headers=%v", identityUnavailable, response.Code, response.Header())
		}
	}
}
