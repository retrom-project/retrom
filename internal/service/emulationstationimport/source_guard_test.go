package emulationstationimport

import (
	"context"
	"errors"
	"testing"
)

type sourceGuardMemory struct {
	state LeaseState
	err   error
}

func (memory sourceGuardMemory) Observe(context.Context, Execution) (LeaseState, error) {
	return memory.state, memory.err
}

func TestSourceGuardRetainsLiveStateAndReadCause(t *testing.T) {
	for _, value := range []struct {
		state LeaseState
		cause error
	}{
		{LeaseActive, nil},
		{LeaseCancelled, ErrExecutionCancelled},
		{LeaseLost, ErrVersionConflict},
		{LeaseDeadline, ErrExpired},
	} {
		guard := NewSourceGuard(sourceGuardMemory{state: value.state})
		if err := guard.Check(t.Context(), Execution{}); !errors.Is(err, value.cause) {
			t.Fatalf("state=%v error=%v", value.state, err)
		}
	}
	cause := errors.New("source state unavailable")
	err := NewSourceGuard(sourceGuardMemory{err: cause}).Check(t.Context(), Execution{})
	if !errors.Is(err, cause) || !errors.Is(err, ErrExecutionObservation) {
		t.Fatalf("lost observation=%v", err)
	}
}
