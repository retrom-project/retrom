package emulationstationimport

import (
	"context"
	"errors"
	model "retrom/internal/model/emulationstationimport"
	"testing"
	"time"
)

type recoveryMemory struct {
	candidate model.LeaseSnapshot
	current   model.LeaseSnapshot
	changes   []model.RecoveryChange
	failure   error
	stage     string
}

func (memory *recoveryMemory) Expired(context.Context, int64, int) ([]model.LeaseSnapshot, error) {
	if memory.stage == "list" {
		return nil, memory.failure
	}
	return []model.LeaseSnapshot{memory.candidate}, nil
}

func (memory *recoveryMemory) WithRecovery(_ context.Context, work func(model.RecoveryScope) error) error {
	if err := work(model.RecoveryScope{Payload: emptyPayloadScope(), Read: memory, Write: memory}); err != nil {
		return err
	}
	if memory.stage == "commit" {
		return memory.failure
	}
	return nil
}

func (memory *recoveryMemory) Current(context.Context, string) (model.LeaseSnapshot, bool, error) {
	if memory.stage == "read" {
		return model.LeaseSnapshot{}, false, memory.failure
	}
	return memory.current, true, nil
}

func (memory *recoveryMemory) Apply(_ context.Context, change model.RecoveryChange) error {
	if memory.stage == "write" {
		return memory.failure
	}
	memory.changes = append(memory.changes, change)
	return nil
}

func recoveryFixture() *recoveryMemory {
	snapshot := leaseFixture().snapshot
	snapshot.JobState = "RUNNING"
	snapshot.WorkerID = "owner"
	snapshot.Attempt = 1
	snapshot.LeaseUntilMS = 900
	snapshot.StartedAtMS = new(int64(1))
	snapshot.DeadlineAtMS = 10000
	return &recoveryMemory{candidate: snapshot, current: snapshot}
}

func TestRecoveryKeepsESBackoffAndBudgetBoundary(t *testing.T) {
	t.Parallel()
	for _, entry := range []struct {
		attempt, deadline, available int64
		want                         string
	}{{1, 2001, 2000, "QUEUED"}, {1, 2000, 0, "FAILED"}, {2, 6001, 6000, "QUEUED"}, {3, 31001, 31000, "QUEUED"}, {4, 200000, 0, "FAILED"}} {
		memory := recoveryFixture()
		memory.candidate.Attempt = entry.attempt
		memory.candidate.DeadlineAtMS = entry.deadline
		memory.current = memory.candidate
		if err := NewRecovery(memory, func() time.Time { return time.UnixMilli(1000) }).Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
		if len(memory.changes) != 1 || memory.changes[0].JobState != entry.want || memory.changes[0].AvailableAtMS != entry.available {
			t.Fatalf("case=%#v changes=%#v", entry, memory.changes)
		}
	}
}

func TestRecoveryPreservesStorageCauses(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"list", "read", "write", "commit"} {
		memory := recoveryFixture()
		memory.stage = stage
		memory.failure = errors.New("recovery unavailable")
		if err := NewRecovery(memory, time.Now).Recover(t.Context()); !errors.Is(err, memory.failure) {
			t.Fatalf("%s cause=%v", stage, err)
		}
	}
}

func TestRecoveryIgnoresReplacedCandidate(t *testing.T) {
	t.Parallel()
	memory := recoveryFixture()
	memory.current.WorkerID = "replacement"
	if err := NewRecovery(memory, func() time.Time { return time.UnixMilli(1000) }).Recover(t.Context()); err != nil || len(memory.changes) != 0 {
		t.Fatalf("error=%v changes=%#v", err, memory.changes)
	}
}

func (*recoveryMemory) Reviews(context.Context, string, int) ([]model.ExecutionReview, error) {
	return nil, nil
}

func (*recoveryMemory) Fence(context.Context, model.LeaseSnapshot, int64) error { return nil }

func (*recoveryMemory) CompleteReview(context.Context, model.ExecutionReviewCompletion) error {
	return nil
}
