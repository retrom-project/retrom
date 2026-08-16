//go:build integration

package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReadinessGatesBusinessRoutesDuringDATIndexing(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	ctx := context.Background()
	if err := server.dependencies.Bootstrap(ctx, server.database, time.Now()); err != nil {
		t.Fatal(err)
	}

	ready := httptest.NewRecorder()
	server.Handler().ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if ready.Code != http.StatusServiceUnavailable ||
		!strings.Contains(ready.Body.String(), `"reasonCode":"DEPENDENCY_INDEXING"`) {
		t.Fatalf("indexing readiness = %d %s", ready.Code, ready.Body.String())
	}
	blocked := httptest.NewRecorder()
	server.Handler().ServeHTTP(blocked, httptest.NewRequest(http.MethodGet, "/api/v1/games", nil))
	if blocked.Code != http.StatusServiceUnavailable ||
		!strings.Contains(blocked.Body.String(), `"code":"SERVICE_NOT_READY"`) ||
		!strings.Contains(blocked.Body.String(), `"reasonCode":"DEPENDENCY_INDEXING"`) {
		t.Fatalf("business gate = %d %s", blocked.Code, blocked.Body.String())
	}

	if err := server.dependencies.BootstrapCatalogs(ctx, server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	ready = httptest.NewRecorder()
	server.Handler().ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if ready.Code != http.StatusOK {
		t.Fatalf("published readiness = %d %s", ready.Code, ready.Body.String())
	}
	session := httptest.NewRecorder()
	server.Handler().ServeHTTP(session, httptest.NewRequest(http.MethodGet, "/api/v1/games", nil))
	if session.Code != http.StatusOK {
		t.Fatalf("published business route = %d %s", session.Code, session.Body.String())
	}
}

func TestStartupReadinessGateDoesNotReprobeForEveryBusinessRequest(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	ctx := context.Background()
	if err := server.dependencies.Bootstrap(ctx, server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := server.dependencies.BootstrapCatalogs(ctx, server.database, time.Now()); err != nil {
		t.Fatal(err)
	}

	ready := httptest.NewRecorder()
	server.Handler().ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if ready.Code != http.StatusOK || !server.startupReady.Load() {
		t.Fatalf("initial readiness = %d %s", ready.Code, ready.Body.String())
	}
	if err := server.readinessDatabase.Close(); err != nil {
		t.Fatal(err)
	}

	business := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/games", nil)
	if !server.requestReady(request.Context(), business, request) {
		t.Fatalf("latched startup gate unexpectedly reprobed: %d %s", business.Code, business.Body.String())
	}

	liveReadiness := httptest.NewRecorder()
	server.Handler().ServeHTTP(liveReadiness, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if liveReadiness.Code != http.StatusServiceUnavailable ||
		!strings.Contains(liveReadiness.Body.String(), `"reasonCode":"DATABASE_UNAVAILABLE"`) {
		t.Fatalf("live readiness after read pool close = %d %s", liveReadiness.Code, liveReadiness.Body.String())
	}
}
