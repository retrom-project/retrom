package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"retrom/internal/config"
	"retrom/internal/service/isolation"
)

func TestWebConfigIsPublicBeforeInitializationAndReturnsOnlyEffectiveTemplate(t *testing.T) {
	t.Parallel()
	for _, mode := range []config.Mode{config.ModeRelease, config.ModeTest} {
		t.Run(string(mode), func(t *testing.T) {
			server := newAuthHTTPServer(t, mode)
			for _, template := range []string{"https://{launchId}.example.com", "https://{launchId}.games.example.com:8443"} {
				server.config.RPGRuntimeOriginTemplate = template
				request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/web-config", nil)
				request.Header.Set("X-Forwarded-Host", "untrusted.example")
				response := httptest.NewRecorder()
				server.Handler().ServeHTTP(response, request)
				if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("web config response = %d %s", response.Code, response.Body.String())
				}
				var fields map[string]string
				if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil {
					t.Fatal(err)
				}
				if len(fields) != 1 || fields["runtimeOriginTemplate"] != template {
					t.Fatalf("unexpected public configuration: %v", fields)
				}
				if len(response.Result().Cookies()) != 0 {
					t.Fatal("public configuration issued credentials")
				}
			}
		})
	}
}

func TestWebConfigRejectsQueryAndIsUnavailableOnRuntimeHosts(t *testing.T) {
	t.Parallel()
	server := newAuthHTTPServer(t, config.ModeRelease)
	server.playDeps.Isolation = isolation.New(nil, "https://{launchId}.example.com", time.Now)
	for _, tc := range []struct {
		url    string
		status int
	}{
		{"https://example.com/api/v1/web-config?runtimeOriginTemplate=evil", http.StatusBadRequest},
		{"https://01980000-0000-7000-8000-000000000091.example.com/api/v1/web-config", http.StatusNotFound},
		{"https://invalid.example.com/api/v1/web-config", http.StatusNotFound},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.url, nil)
		server.Handler().ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("%s: status = %d, want %d", tc.url, response.Code, tc.status)
		}
	}
}
