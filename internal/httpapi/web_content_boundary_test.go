package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	application "retrom/internal/service/launch"
)

type isolatedContentBoundary struct {
	application.ContentReader
	content application.ContentView
}

func (fixture isolatedContentBoundary) ProductContent(context.Context, string, string, bool) (application.ContentRecord, bool, error) {
	return application.ContentRecord{Session: application.SessionRecord{State: "ACTIVE", HardExpiresAtMS: 2000}, Content: fixture.content}, true, nil
}

func (fixture isolatedContentBoundary) Project(_ context.Context, _ string, authorize application.ConfigAuthorization) (application.ConfigSnapshot, bool, error) {
	source := application.ConfigSource{
		Purpose: "PRODUCT", State: "ACTIVE", Delivery: "ISOLATED_WEB_PROJECT", HardEnd: 2000, Version: 1,
	}
	if err := authorize(source); err != nil {
		return application.ConfigSnapshot{}, false, err
	}
	return application.ConfigSnapshot{Authority: application.ConfigAuthority{Source: source}, Files: fixture.files()}, true, nil
}

func (fixture isolatedContentBoundary) files() []application.ConfigFile {
	return []application.ConfigFile{{LogicalName: "index.html", Format: fixture.content.Format, Digest: fixture.content.Digest}}
}

func TestIsolatedProgramsCannotUseGenericApplicationContentRoutes(t *testing.T) {
	for _, format := range []string{"RPG_MAKER_PROJECT", "TYRANOSCRIPT_PROJECT"} {
		t.Run(format, func(t *testing.T) {
			fixture := isolatedContentBoundary{content: application.ContentView{
				Format: format, DeliveryProfile: "ISOLATED_WEB_PROJECT",
				Digest: strings.Repeat("a", 64), ProviderID: "provider", TargetID: "target", CoreID: "core", BundleSHA256: strings.Repeat("b", 64),
			}}
			now := func() time.Time { return time.UnixMilli(1000) }
			matches := func(capability string, _ []byte) bool { return capability == "valid" }
			server := &Server{playDeps: PlayDependencies{Launcher: application.New(application.ServiceDependencies{
				Content: application.NewContentAccess(fixture, now, matches), Projects: application.NewProjectQueries(fixture, now, matches),
			})}}
			gameIdentity, err := application.ContentIdentity(fixture.content)
			if err != nil {
				t.Fatal(err)
			}
			projectIdentity, err := application.ProjectIdentity(fixture.files())
			if err != nil {
				t.Fatal(err)
			}
			for _, route := range []struct {
				identity string
				handler  http.HandlerFunc
			}{
				{gameIdentity, server.launchGame}, {projectIdentity, server.launchProjectFile},
			} {
				request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/runtime/content/index.html", nil)
				request.SetPathValue("contentIdentity", route.identity)
				request.SetPathValue("logicalName", "index.html")
				request.SetPathValue("projectPath", "index.html")
				request = request.WithContext(context.WithValue(request.Context(), runtimeGrantsKey{}, []runtimeContentGrant{{LaunchID: "launch", Capability: "valid"}}))
				response := httptest.NewRecorder()
				route.handler(response, request)
				if response.Code != http.StatusUnauthorized {
					t.Fatalf("native app-origin route status = %d", response.Code)
				}
			}
		})
	}
}
