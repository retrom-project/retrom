//go:build integration

package httpapi

import "testing"

func TestRepeatedLaunchesReuseRuntimeCookie(t *testing.T) {
	t.Parallel()
	server, item := newCheckpointReviewHTTPFixture(t)
	_, first := createCheckpointPreviewHTTP(t, server, item, nil)
	for range 40 {
		_, next := createCheckpointPreviewHTTP(t, server, item, nil)
		if next.Name != first.Name || next.Value != first.Value {
			t.Fatal("starting another game run allocated a new runtime credential")
		}
	}
}
