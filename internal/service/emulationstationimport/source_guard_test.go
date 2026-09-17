package emulationstationimport

import (
	"context"
	"errors"
	"testing"

	model "retrom/internal/model/emulationstationimport"
)

type sourceGuardMemory struct {
	state model.LeaseState
	err   error
}

func (memory sourceGuardMemory) Observe(context.Context, model.Execution) (model.LeaseState, error) {
	return memory.state, memory.err
}

func TestSourceGuardRetainsLiveStateAndReadCause(t *testing.T) {
	for _, value := range []struct {
		state model.LeaseState
		cause error
	}{
		{model.LeaseActive, nil},
		{model.LeaseCancelled, ErrExecutionCancelled},
		{model.LeaseLost, model.ErrVersionConflict},
		{model.LeaseDeadline, model.ErrExpired},
	} {
		guard := NewSourceGuard(sourceGuardMemory{state: value.state})
		if err := guard.Check(t.Context(), model.Execution{}); !errors.Is(err, value.cause) {
			t.Fatalf("state=%v error=%v", value.state, err)
		}
	}
	cause := errors.New("source state unavailable")
	err := NewSourceGuard(sourceGuardMemory{err: cause}).Check(t.Context(), model.Execution{})
	if !errors.Is(err, cause) || !errors.Is(err, ErrExecutionObservation) {
		t.Fatalf("lost observation=%v", err)
	}
}
