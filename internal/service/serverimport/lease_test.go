package serverimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/serverimport"
)

type leaseMemory struct {
	snapshot model.LeaseSnapshot
	claim    model.LeaseClaim
	touch    model.LeaseTouch
	writes   int
	lateErr  error
}

func (memory *leaseMemory) CommitWrite(_ context.Context, work func(model.LeaseRecords) error) error {
	if err := work(memory); err != nil {
		return err
	}
	return memory.lateErr
}

func (memory *leaseMemory) Next(context.Context, int64) (model.LeaseSnapshot, bool, error) {
	return memory.snapshot, true, nil
}

func (memory *leaseMemory) Current(context.Context, string) (model.LeaseSnapshot, error) {
	return memory.snapshot, nil
}

func (memory *leaseMemory) Claim(_ context.Context, plan model.LeaseClaim) error {
	memory.claim = plan
	memory.writes++
	return nil
}

func (memory *leaseMemory) Touch(_ context.Context, plan model.LeaseTouch) error {
	memory.touch = plan
	memory.writes++
	return nil
}

func TestClaimUsesNewOwnerAndPreservesExecutionDeadline(t *testing.T) {
	deadline := int64(500)
	memory := &leaseMemory{snapshot: model.LeaseSnapshot{Work: model.Work{ImportID: "import", JobID: "job", Owner: "old", Execution: 2}, State: "RUNNING", ImportState: "RUNNING", Maximum: 4, Attempt: 1, Deadline: &deadline}}
	service := NewLeases(memory, func() time.Time { return time.UnixMilli(100) })
	first, found, err := service.Claim(t.Context())
	if err != nil || !found || first.Owner == "" || first.Owner == "old" || first.Execution != 2 || first.DeadlineAtMS != deadline || len(memory.claim.RecoveryEvent) == 0 {
		t.Fatalf("recovered claim: %+v %v", first, err)
	}
	second, _, err := service.Claim(t.Context())
	if err != nil || first.Owner == second.Owner {
		t.Fatalf("claim owner reused: %+v %v", second, err)
	}
	memory.snapshot.State = "QUEUED"
	memory.snapshot.ImportState = "QUEUED"
	memory.snapshot.Deadline = nil
	created, _, err := service.Claim(t.Context())
	if err != nil || created.DeadlineAtMS != 100+(8*time.Hour).Milliseconds() || len(memory.claim.RecoveryEvent) != 0 {
		t.Fatalf("new execution budget: %+v %v", created, err)
	}
}

func TestLeaseTouchRejectsDifferentExecutionOwnerAndCancellation(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*model.LeaseSnapshot)
		want   error
	}{
		{"execution", func(s *model.LeaseSnapshot) { s.Work.Execution++ }, model.ErrLeaseLost},
		{"owner", func(s *model.LeaseSnapshot) { s.Work.Owner = "replacement" }, model.ErrLeaseLost},
		{"expired", func(s *model.LeaseSnapshot) { v := int64(100); s.LeaseUntil = &v }, model.ErrLeaseLost},
		{"cancel", func(s *model.LeaseSnapshot) { s.State = "CANCEL_REQUESTED" }, model.ErrWorkerCancelled},
		{"finished", func(s *model.LeaseSnapshot) { s.State = "SUCCEEDED" }, model.ErrLeaseLost},
	} {
		t.Run(test.name, func(t *testing.T) {
			lease := int64(200)
			unit := model.Work{ImportID: "import", JobID: "job", Owner: "worker", Execution: 1}
			memory := &leaseMemory{snapshot: model.LeaseSnapshot{Work: unit, State: "RUNNING", ImportState: "RUNNING", LeaseUntil: &lease}}
			test.change(&memory.snapshot)
			err := NewLeases(memory, func() time.Time { return time.UnixMilli(100) }).Progress(t.Context(), unit, "INSTALLING", 0, 1)
			if !errors.Is(err, test.want) || memory.writes != 0 {
				t.Fatalf("stale progress: %v writes=%d", err, memory.writes)
			}
		})
	}
}

func TestClaimCommitFailureDoesNotReleaseWork(t *testing.T) {
	memory := &leaseMemory{snapshot: model.LeaseSnapshot{State: "QUEUED", ImportState: "QUEUED", Maximum: 4}, lateErr: context.Canceled}
	unit, found, err := NewLeases(memory, time.Now).Claim(t.Context())
	if !errors.Is(err, context.Canceled) || found || unit.Owner != "" {
		t.Fatalf("uncommitted work escaped: %+v %v %v", unit, found, err)
	}
}
