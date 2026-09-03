package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderRouteDelegatesOnlyNewContentAddressedBoundary(t *testing.T) {
	server := newTestServer(t).WithRuntimeProviderHandler(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Provider-Path", request.URL.Path)
		writer.WriteHeader(http.StatusNoContent)
	}))
	server.startupReady.Store(true)
	bundle := strings.Repeat("a", 64)
	path := "/runtime/providers/fixture/" + bundle + "/assets/core.wasm"
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		response := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(context.Background(), method, path, nil)
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusNoContent || response.Header().Get("X-Provider-Path") != path {
			t.Fatalf("%s response = %d %v", method, response.Code, response.Header())
		}
	}
	for _, legacy := range []string{
		"/runtime/emulatorjs/4.2.3/data/loader.js",
		"/runtime/retrom-runtime/0.12.0/client.mjs",
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, legacy, nil)
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("legacy route %s = %d %q", legacy, response.Code, response.Body.String())
		}
	}
}
