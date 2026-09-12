package emulationstationimport

import (
	"context"
	"errors"
	"testing"
	"time"
)

type executionMemory struct {
	before    LeaseSnapshot
	finish    ExecutionFinish
	terminal  int64
	err       error
	committed bool
}

func (memory *executionMemory) WithExecution(_ context.Context, run func(ExecutionScope) error) error {
	if err := run(ExecutionScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	if memory.err != nil {
		return memory.err
	}
	memory.committed = true
	return nil
}

func (memory *executionMemory) Current(context.Context, string) (LeaseSnapshot, bool, error) {
	return memory.before, true, nil
}

func (memory *executionMemory) TerminalCount(context.Context, string) (int64, error) {
	return memory.terminal, nil
}

func (memory *executionMemory) Finish(_ context.Context, change ExecutionFinish) error {
	memory.finish = change
	return nil
}

func newExecutionMemory() *executionMemory {
	started := int64(1000)
	return &executionMemory{before: LeaseSnapshot{
		Execution: Execution{
			JobID: "job", ImportID: "plan", Kind: "SERVER_EMULATIONSTATION_SCAN", WorkerID: "owner",
			ExecutionNo: 1, Attempt: 1, DeadlineAtMS: 300000, ReleaseYearMax: 2027,
		},
		JobState: "RUNNING", ImportState: "SCANNING", JobVersion: 2, ImportVersion: 3, MaxAttempts: 4,
		LeaseUntilMS: 61000, StartedAtMS: &started,
	}}
}

func TestExecutionControlPreservesRetryBudgetAndCommit(t *testing.T) {
	memory := newExecutionMemory()
	service := NewExecutionControl(memory, func() time.Time { return time.UnixMilli(2000) })
	state, err := service.Fail(
		t.Context(),
		memory.before.Execution,
		ExecutionFailure{Code: "INTERNAL_ERROR", Retryable: true},
	)
	if err != nil || state != "QUEUED" || !memory.committed || memory.finish.AvailableAtMS != 3000 ||
		memory.finish.Before.DeadlineAtMS != 300000 || memory.finish.Before.ReleaseYearMax != 2027 {
		t.Fatalf("failure result=%s error=%v change=%#v", state, err, memory.finish)
	}
	cause := errors.New("commit failure")
	memory.err = cause
	state, err = service.Fail(
		t.Context(),
		memory.before.Execution,
		ExecutionFailure{Code: "INTERNAL_ERROR", Retryable: true},
	)
	if state != "" || !errors.Is(err, cause) {
		t.Fatalf("uncommitted failure result=%s error=%v", state, err)
	}
}

func TestExecutionControlRejectsLostAndExpiredCancellation(t *testing.T) {
	for _, mutation := range []string{"owner", "lease", "deadline"} {
		t.Run(mutation, func(t *testing.T) {
			memory := newExecutionMemory()
			memory.before.JobState = "CANCEL_REQUESTED"
			memory.before.ImportState = "CANCEL_REQUESTED"
			unit := memory.before.Execution
			now := int64(2000)
			switch mutation {
			case "owner":
				memory.before.WorkerID = "replacement"
			case "lease":
				now = 61000
			case "deadline":
				now = 300000
			}
			closed, err := NewExecutionControl(memory, func() time.Time { return time.UnixMilli(now) }).CloseCancelled(
				t.Context(),
				unit,
			)
			if closed || err == nil || memory.finish.JobState != "" {
				t.Fatalf("unauthorized cancellation=%v error=%v change=%#v", closed, err, memory.finish)
			}
		})
	}
}

func (memory *executionMemory) Reviews(context.Context, string, int) ([]ExecutionReview, error) {
	return nil, nil
}
func (memory *executionMemory) Fence(context.Context, LeaseSnapshot, int64) error { return nil }
func (memory *executionMemory) CompleteReview(context.Context, ExecutionReviewCompletion) error {
	return nil
}
