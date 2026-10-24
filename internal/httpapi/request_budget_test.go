package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestContentReplacementSubmissionUsesOrdinaryHTTPBudget(t *testing.T) {
	t.Parallel()
	for _, item := range []struct {
		pattern string
		want    time.Duration
	}{
		{"POST /api/v1/admin/games/{gameId}/content-replacement", 10 * time.Minute},
		{"POST /api/v1/admin/games/{gameId}/parents", 10 * time.Minute},
		{"GET /api/v1/games", 10 * time.Minute},
		{"", 10 * time.Minute},
	} {
		r := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
			"/api/v1/admin/games/example/content-replacement", http.NoBody)
		r.Pattern = item.pattern
		before := time.Now()
		bounded, cancel := boundedRequest(r)
		deadline, exists := bounded.Context().Deadline()
		cancel()
		if !exists || deadline.Before(before.Add(item.want)) || deadline.After(time.Now().Add(item.want)) {
			t.Errorf("matched route %q budget=%s want=%s", item.pattern, deadline.Sub(before), item.want)
		}
	}
}

func TestContentReplacementPreservesCallerCancellation(t *testing.T) {
	t.Parallel()
	ctx, stop := context.WithCancel(t.Context())
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/", http.NoBody)
	r.Pattern = "POST /api/v1/admin/games/{gameId}/content-replacement"
	bounded, cancel := boundedRequest(r)
	defer cancel()
	stop()
	if !errors.Is(bounded.Context().Err(), context.Canceled) {
		t.Fatal("long content preparation detached request cancellation")
	}
}
