package launch

import (
	"context"
	"errors"
	"testing"
)

func TestValidationLifecycleCloseCancelsAndJoinsBeforeRejectingRegistration(t *testing.T) {
	runs := newValidationWorkerRuns()
	ctx, cancel := context.WithCancelCause(t.Context())
	done, accepted := runs.register(cancel)
	if !accepted {
		t.Fatal("new lifecycle rejected work")
	}
	joined := make(chan struct{})
	go func() { runs.Close(); close(joined) }()
	<-ctx.Done()
	if !errors.Is(context.Cause(ctx), ErrValidationWorkerClosed) {
		t.Fatalf("close cause: %v", context.Cause(ctx))
	}
	select {
	case <-joined:
		t.Fatal("Close returned before work exited")
	default:
	}
	done()
	<-joined
	next, nextCancel := context.WithCancelCause(t.Context())
	if _, accepted := runs.register(nextCancel); accepted {
		t.Fatal("registration after Close")
	}
	if !errors.Is(context.Cause(next), ErrValidationWorkerClosed) {
		t.Fatal("rejected run lacks close cause")
	}
	runs.Close()
}
