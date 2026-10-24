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
		"assemble-resource": 2 * time.Minute,
		"detect-scummvm":    time.Minute,
		"prepare":           30 * time.Second,
	} {
		if got := commandTimeout(command); got != want {
			t.Errorf("%s budget=%s want=%s", command, got, want)
		}
	}
}

func TestNormalizationRetainsCallerDeadlineAndSharedCLI(t *testing.T) {
	pool := fixturePool(t)
	var before, after workerResult
	if err := pool.Call(t.Context(), "valid", nil, &before); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := pool.Call(ctx, "normalize-content", map[string]any{"delay": 0.1}, &workerResult{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("normalization ignored caller deadline: %v", err)
	}
	if err := pool.Call(t.Context(), "valid", nil, &after); err != nil || before.PID != after.PID {
		t.Fatalf("canceling normalization disrupted shared runtime: before=%+v after=%+v err=%v", before, after, err)
	}
}
