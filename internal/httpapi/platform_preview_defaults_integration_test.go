//go:build integration

package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dependencypersistence "retrom/internal/persistence/dependencies"
	dependencyservice "retrom/internal/service/dependencies"
	"retrom/internal/testsupport"
)

func TestDefaultCorePreviewOptionalLimit(t *testing.T) {
	server := newTestServer(t)
	if err := dependencyservice.New(server.dependencies, dependencypersistence.New(server.database)).Bootstrap(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := dependencyservice.New(server.dependencies, dependencypersistence.New(server.database)).BootstrapCatalogs(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	seedMovableGame(t, server)
	instance := testsupport.MustPlatformInstanceID(t, server.database, "gbc/gambatte")
	handler := server.Handler()
	cookie, csrf := testSessionCredentials()
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"omitted", `{"coreId":"mgba"}`, 200},
		{"minimum", `{"coreId":"mgba","limit":1}`, 200},
		{"maximum", `{"coreId":"mgba","limit":100}`, 200},
		{"zero", `{"coreId":"mgba","limit":0}`, 400},
		{"negative", `{"coreId":"mgba","limit":-1}`, 400},
		{"overflow", `{"coreId":"mgba","limit":101}`, 400},
		{"null", `{"coreId":"mgba","limit":null}`, 400},
		{"string", `{"coreId":"mgba","limit":"50"}`, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/platform-instances/"+instance+"/default-core-preview", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("If-Match", `"v1"`)
			setCSRFCredentials(request, cookie, csrf)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.status, response.Body.String())
			}
		})
	}
}
