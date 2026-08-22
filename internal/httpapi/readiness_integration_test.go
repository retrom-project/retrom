//go:build integration

package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"retrom/internal/testassert"
)

func TestReadinessGatesBusinessRoutesDuringDATIndexing(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	ctx := context.Background()
	if err := server.dependencies.Bootstrap(ctx, server.database, time.Now()); err != nil {
		t.Fatal(err)
	}

	ready := httptest.NewRecorder()
	server.Handler().ServeHTTP(ready, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health/ready", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return ready.Code != http.StatusServiceUnavailable }, func() bool { return !strings.Contains(ready.Body.String(), `"reasonCode":"DEPENDENCY_INDEXING"`) }), "indexing readiness = %d %s", ready.Code, ready.Body.String())
	blocked := httptest.NewRecorder()
	server.Handler().ServeHTTP(blocked, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/games", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return blocked.Code != http.StatusServiceUnavailable }, func() bool { return !strings.Contains(blocked.Body.String(), `"code":"SERVICE_NOT_READY"`) }, func() bool { return !strings.Contains(blocked.Body.String(), `"reasonCode":"DEPENDENCY_INDEXING"`) }), "business gate = %d %s", blocked.Code, blocked.Body.String())

	if err := server.dependencies.BootstrapCatalogs(ctx, server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	ready = httptest.NewRecorder()
	server.Handler().ServeHTTP(ready, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health/ready", nil))
	testassert.Falsef(t, ready.Code != http.StatusOK, "published readiness = %d %s", ready.Code, ready.Body.String())
	session := httptest.NewRecorder()
	server.Handler().ServeHTTP(session, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/games", nil))
	testassert.Falsef(t, session.Code != http.StatusOK, "published business route = %d %s", session.Code, session.Body.String())
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
	server.Handler().ServeHTTP(ready, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health/ready", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return ready.Code != http.StatusOK }, func() bool { return !server.startupReady.Load() }), "initial readiness = %d %s", ready.Code, ready.Body.String())
	if err := server.readinessDatabase.Close(); err != nil {
		t.Fatal(err)
	}

	business := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/games", nil)
	testassert.Truef(t, server.requestReady(request.Context(), business, request), "latched startup gate unexpectedly reprobed: %d %s", business.Code, business.Body.String())

	liveReadiness := httptest.NewRecorder()
	server.Handler().ServeHTTP(liveReadiness, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health/ready", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return liveReadiness.Code != http.StatusServiceUnavailable }, func() bool {
		return !strings.Contains(liveReadiness.Body.String(), `"reasonCode":"DATABASE_UNAVAILABLE"`)
	}), "live readiness after read pool close = %d %s", liveReadiness.Code, liveReadiness.Body.String())
}
