package emulationstationimport

import (
	"errors"
	model "retrom/internal/model/emulationstationimport"
	"testing"
	"time"
)

func TestExecutionFailurePolicyKeepsExistingTerminalAndBackoffBoundaries(t *testing.T) {
	for _, test := range []struct {
		name                             string
		attempt, max, deadline, terminal int64
		retryable                        bool
		state, code                      string
	}{
		{"permanent", 1, 4, 100000, 0, false, "FAILED", "SOURCE_CHANGED"},
		{"terminal item", 1, 4, 100000, 1, true, "FAILED", "SOURCE_CHANGED"},
		{"attempt maximum", 4, 4, 100000, 0, true, "FAILED", "EMULATIONSTATION_WORKER_ATTEMPTS_EXHAUSTED"},
		{"first backoff cutoff", 1, 4, 3000, 0, true, "FAILED", "EMULATIONSTATION_EXECUTION_TIMEOUT"},
		{"first backoff before cutoff", 1, 4, 3001, 0, true, "QUEUED", "SOURCE_CHANGED"},
		{"second backoff cutoff", 2, 4, 7000, 0, true, "FAILED", "EMULATIONSTATION_EXECUTION_TIMEOUT"},
		{"third backoff cutoff", 3, 4, 32000, 0, true, "FAILED", "EMULATIONSTATION_EXECUTION_TIMEOUT"},
	} {
		t.Run(test.name, func(t *testing.T) {
			memory := newExecutionMemory()
			memory.before.Attempt, memory.before.MaxAttempts, memory.before.DeadlineAtMS = test.attempt, test.max, test.deadline
			memory.terminal = test.terminal
			state, err := NewExecutionControl(memory, func() time.Time { return time.UnixMilli(2000) }).Fail(t.Context(),
				memory.before.Execution, model.ExecutionFailure{Code: "SOURCE_CHANGED", Retryable: test.retryable})
			if err != nil || state != test.state || memory.finish.Code != test.code {
				t.Fatalf(
					"state=%s error=%v change=%#v",
					state,
					err,
					memory.finish,
				)
			}
		})
	}
}

func TestExecutionObservationAndCancellationCommitPreserveCauses(t *testing.T) {
	memory := newExecutionMemory()
	service := NewExecutionControl(memory, func() time.Time { return time.UnixMilli(2000) })
	state, err := service.Observe(t.Context(), memory.before.Execution)
	if err != nil || state != model.LeaseActive {
		t.Fatalf("observation=%v error=%v", state, err)
	}
	_, err = service.Fail(t.Context(), model.Execution{JobID: "job"}, model.ExecutionFailure{Code: "INTERNAL_ERROR", Retryable: true})
	if !errors.Is(err, model.ErrVersionConflict) || memory.finish.JobState != "" {
		t.Fatalf("stale identity failure=%v", err)
	}
	memory.before.JobState, memory.before.ImportState = "CANCEL_REQUESTED", "CANCEL_REQUESTED"
	memory.err = errors.New("commit acknowledgement failure")
	closed, err := service.CloseCancelled(t.Context(), memory.before.Execution)
	if closed || !errors.Is(err, memory.err) {
		t.Fatalf("commit cancellation=%v error=%v", closed, err)
	}
	state, err = service.Observe(t.Context(), memory.before.Execution)
	if state != model.LeaseLost || !errors.Is(err, memory.err) {
		t.Fatalf("failed observation=%v error=%v", state, err)
	}
}
