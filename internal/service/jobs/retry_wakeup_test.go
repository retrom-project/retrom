package jobs

import (
	"context"
	"testing"
	"time"
)

func TestRetryWakesOnlyRegisteredKindAndCopiesBindings(t *testing.T) {
	var got string
	handlers := map[string]RetryWakeup{"attachment": func(_ context.Context, id string) { got = id }}
	original := New(&memoryJobs{}, time.Now)
	service := original.WithRetryWakeups(handlers)
	handlers["attachment"] = func(context.Context, string) { t.Fatal("binding changed after construction") }
	original.WakeRetry(t.Context(), Result{Kind: "attachment", JobID: "unconfigured"})
	service.WakeRetry(t.Context(), Result{Kind: "other", JobID: "unrelated"})
	if got != "" {
		t.Fatalf("unrelated wake=%s", got)
	}
	service.WakeRetry(t.Context(), Result{Kind: "attachment", JobID: "job"})
	if got != "job" {
		t.Fatalf("wake=%s", got)
	}
}
