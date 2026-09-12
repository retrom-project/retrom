package emulationstationimport

import (
	"context"
	"errors"
	"testing"
	"time"
)

type leaseMemory struct {
	snapshot LeaseSnapshot
	found    bool
	claim    *ClaimLease
	renew    *RenewLease
	failure  error
	stage    string
}

func (memory *leaseMemory) WithLease(_ context.Context, work func(LeaseScope) error) error {
	if err := work(LeaseScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	if memory.stage == "commit" {
		return memory.failure
	}
	return nil
}

func (memory *leaseMemory) Next(context.Context, int64) (LeaseSnapshot, bool, error) {
	if memory.stage == "read" {
		return LeaseSnapshot{}, false, memory.failure
	}
	return memory.snapshot, memory.found, nil
}

func (memory *leaseMemory) Current(ctx context.Context, _ string) (LeaseSnapshot, bool, error) {
	return memory.Next(ctx, 0)
}

func (memory *leaseMemory) Claim(_ context.Context, plan ClaimLease) error {
	if memory.stage == "write" {
		return memory.failure
	}
	memory.claim = &plan
	return nil
}

func (memory *leaseMemory) Renew(_ context.Context, plan RenewLease) error {
	if memory.stage == "write" {
		return memory.failure
	}
	memory.renew = &plan
	return nil
}

func leaseFixture() *leaseMemory {
	return &leaseMemory{found: true, snapshot: LeaseSnapshot{Execution: Execution{JobID: "job", ImportID: "import", Kind: "SERVER_EMULATIONSTATION_SCAN", RootID: "root", RootDigest: "digest", CreatedByUserID: "actor", ExecutionNo: 1, ReleaseYearMax: 2027}, JobState: "QUEUED", ImportState: "SCANNING", JobVersion: 1, ImportVersion: 1, MaxAttempts: 4}}
}

func TestLeasesClaimFreezesBudgetAndUniqueAttemptOwner(t *testing.T) {
	t.Parallel()
	owners := make(map[string]bool)
	for _, kind := range []string{"SERVER_EMULATIONSTATION_SCAN", "SERVER_EMULATIONSTATION_IMPORT"} {
		memory := leaseFixture()
		memory.snapshot.Kind = kind
		if kind == "SERVER_EMULATIONSTATION_IMPORT" {
			memory.snapshot.ImportState = "QUEUED"
		}
		unit, found, err := NewLeases(memory, func() time.Time { return time.UnixMilli(1000) }).Claim(t.Context())
		if err != nil || !found || unit.WorkerID == "" || unit.Attempt != 1 || unit.DeadlineAtMS != 1000+int64((8*time.Hour)/time.Millisecond) || unit.ReleaseYearMax != 2027 || memory.claim == nil {
			t.Fatalf("lease=%#v found=%v error=%v", unit, found, err)
		}
		if owners[unit.WorkerID] {
			t.Fatal("two claims reused one worker identity")
		}
		owners[unit.WorkerID] = true
		if memory.claim.UntilMS != 61000 || memory.claim.NowMS != 1000 {
			t.Fatalf("claim=%#v", memory.claim)
		}
	}
}

func TestLeasesRenewRejectsReplacedExpiredAndDeadlineOwners(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"replaced", "lease expiry", "deadline", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			memory := leaseFixture()
			memory.snapshot.JobState = "RUNNING"
			memory.snapshot.WorkerID = "owner"
			memory.snapshot.Attempt = 1
			memory.snapshot.DeadlineAtMS = 2000
			memory.snapshot.LeaseUntilMS = 1500
			unit := memory.snapshot.Execution
			want := LeaseLost
			switch kind {
			case "replaced":
				memory.snapshot.WorkerID = "other"
			case "lease expiry":
				memory.snapshot.LeaseUntilMS = 1000
			case "deadline":
				memory.snapshot.DeadlineAtMS = 1000
				unit.DeadlineAtMS = 1000
				want = LeaseDeadline
			case "cancelled":
				memory.snapshot.JobState = "CANCEL_REQUESTED"
				memory.snapshot.ImportState = "CANCEL_REQUESTED"
				want = LeaseCancelled
			}
			state, err := NewLeases(memory, func() time.Time { return time.UnixMilli(1000) }).Renew(t.Context(), unit)
			if err != nil || state != want || memory.renew != nil {
				t.Fatalf("renew state=%s error=%v write=%#v", state, err, memory.renew)
			}
		})
	}
}

func TestLeasesNeverReturnClaimWhenStorageFails(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"read", "write", "commit"} {
		memory := leaseFixture()
		memory.stage = stage
		memory.failure = errors.New("lease storage failure")
		unit, found, err := NewLeases(memory, func() time.Time { return time.UnixMilli(1000) }).Claim(t.Context())
		if unit.JobID != "" || found || !errors.Is(err, memory.failure) {
			t.Fatalf("%s unit=%#v found=%v error=%v", stage, unit, found, err)
		}
	}
}
