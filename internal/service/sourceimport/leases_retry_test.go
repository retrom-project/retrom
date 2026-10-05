package sourceimport

import (
	"context"
	"testing"
	"time"
)

type replayedLease struct {
	leaseFake
	absent bool
}

func (fake *replayedLease) WithLease(_ context.Context, run func(LeaseRecords) error) error {
	if err := run(fake); err != nil {
		return err
	}
	// The first transaction rolled back; another worker acquired the last job.
	fake.absent = true
	return run(fake)
}

func (fake *replayedLease) Next(ctx context.Context, kind string, now int64) (LeaseCandidate, bool, error) {
	if fake.absent {
		return LeaseCandidate{}, false, nil
	}
	return fake.leaseFake.Next(ctx, kind, now)
}

func TestLeaseRetryDoesNotReturnRolledBackWorker(t *testing.T) {
	fake := &replayedLease{leaseFake: leaseFake{candidate: leaseCandidate()}}
	unit, found, err := NewLeases(fake, func() time.Time { return time.UnixMilli(10) }).Claim(t.Context(), "IMPORT_RECEIVE")
	if err != nil || found || unit != (Work{}) {
		t.Fatalf("rolled-back lease escaped retry: unit=%+v found=%v error=%v", unit, found, err)
	}
}
