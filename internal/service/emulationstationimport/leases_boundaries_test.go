package emulationstationimport

import (
	"errors"
	"math"
	model "retrom/internal/model/emulationstationimport"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLeasesClaimChecksEntropyBeforeWriting(t *testing.T) {
	memory := leaseFixture()
	unit, found, err := func() (model.Execution, bool, error) {
		uuid.SetRand(&identityEntropy{})
		defer uuid.SetRand(nil)
		return NewLeases(memory, func() time.Time { return time.UnixMilli(1000) }).Claim(t.Context())
	}()
	if !errors.Is(err, errIdentityEntropy) || found || unit.JobID != "" || memory.claim != nil {
		t.Fatalf("claim=%#v found=%v error=%v", unit, found, err)
	}
}

func TestLeasesRejectInvalidCandidateBudgets(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"attempt", "job version", "plan version", "available", "state", "execution", "unpaired deadline", "unpaired start", "overflow"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			memory := leaseFixture()
			now := int64(1000)
			switch scenario {
			case "attempt":
				memory.snapshot.Attempt = memory.snapshot.MaxAttempts
			case "job version":
				memory.snapshot.JobVersion = math.MaxInt64
			case "plan version":
				memory.snapshot.ImportVersion = 0
			case "available":
				memory.snapshot.AvailableAtMS = 1001
			case "state":
				memory.snapshot.JobState = "FAILED"
			case "execution":
				memory.snapshot.ExecutionNo = 0
			case "unpaired deadline":
				memory.snapshot.DeadlineAtMS = 2000
			case "unpaired start":
				memory.snapshot.StartedAtMS = new(int64(1))
			case "overflow":
				now = math.MaxInt64 - (8 * time.Hour).Milliseconds() + 1
			}
			unit, found, err := NewLeases(memory, func() time.Time { return time.UnixMilli(now) }).Claim(t.Context())
			if !errors.Is(err, model.ErrInvalid) || found || unit.JobID != "" || memory.claim != nil {
				t.Fatalf("claim=%#v found=%v error=%v", unit, found, err)
			}
		})
	}
}

func TestLeasesReturnNoWorkWhenQueueIsEmpty(t *testing.T) {
	t.Parallel()
	memory := leaseFixture()
	memory.found = false
	unit, found, err := NewLeases(memory, time.Now).Claim(t.Context())
	if err != nil || found || unit.JobID != "" || memory.claim != nil {
		t.Fatalf("claim=%#v found=%v error=%v", unit, found, err)
	}
}
