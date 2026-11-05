package runtimeclient

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNormalizationHasAnIndependentBoundedPreparationBudget(t *testing.T) {
	t.Parallel()
	for command, want := range map[string]time.Duration{
		"normalize-content": time.Hour,
		"detect-scummvm":    time.Minute,
		"prepare":           30 * time.Second,
	} {
		if got := commandTimeout(command); got != want {
			t.Errorf("%s budget=%s want=%s", command, got, want)
		}
	}
}

func TestNormalizationRetainsCallerDeadline(t *testing.T) {
	fake := newGatedHost()
	pool := newHostPool(t.Context(), fake)
	t.Cleanup(pool.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := pool.Call(ctx, "normalize-content", gatedValue{Value: 0}, &gatedValue{}); !errors.Is(err,
		context.DeadlineExceeded) {
		t.Fatalf("normalization ignored caller deadline: %v", err)
	}
	fake.release(0)
	fake.release(1)
	if err := pool.Call(t.Context(), "prepare", gatedValue{Value: 1}, &gatedValue{}); err != nil {
		t.Fatalf("canceling normalization disrupted the runtime: %v", err)
	}
}
