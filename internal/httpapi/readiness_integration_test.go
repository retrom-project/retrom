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
	server.Handler().ServeHTTP(blocked, httptest.NewRequest(http.MethodGet, "/api/v1/auth/context", nil))
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
	server.Handler().ServeHTTP(session, httptest.NewRequest(http.MethodGet, "/api/v1/auth/context", nil))
	if session.Code != http.StatusOK {
		t.Fatalf("published business route = %d %s", session.Code, session.Body.String())
	}
}
