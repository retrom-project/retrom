package serverimport

import (
	"context"
	"testing"
	"time"
)

type retriedLease struct {
	leaseMemory
	absent bool
}

func (memory *retriedLease) WithWrite(_ context.Context, work func(LeaseRecords) error) error {
	if err := work(memory); err != nil {
		return err
	}
	// Another worker won the lease while the first attempt rolled back.
	memory.absent = true
	return work(memory)
}

func (memory *retriedLease) Next(ctx context.Context, now int64) (LeaseSnapshot, bool, error) {
	if memory.absent {
		return LeaseSnapshot{}, false, nil
	}
	return memory.leaseMemory.Next(ctx, now)
}

func TestClaimRetryDoesNotReturnRolledBackLease(t *testing.T) {
	memory := &retriedLease{leaseMemory: leaseMemory{snapshot: LeaseSnapshot{
		State: "QUEUED", ImportState: "QUEUED", Maximum: 4,
	}}}
	unit, found, err := NewLeases(memory, time.Now).Claim(t.Context())
	if err != nil || found || unit != (Work{}) {
		t.Fatalf("rolled-back claim escaped: unit=%+v found=%v error=%v", unit, found, err)
	}
}
