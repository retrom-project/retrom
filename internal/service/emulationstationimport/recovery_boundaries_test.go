package emulationstationimport

import (
	"errors"
	"math"
	model "retrom/internal/model/emulationstationimport"
	"testing"
	"time"
)

func TestRecoveryRevalidatesLeaseAndQueuedBudget(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"unexpired", "queued fresh", "queued deadline", "queued attempts", "cancel expired", "cancel live", "separate start pointer"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			assertRecoveryEligibility(t, scenario)
		})
	}
}

func TestRecoveryRejectsInvalidVersionAndBudgetIdentity(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"job version", "plan version", "attempt", "execution", "missing start", "missing deadline", "missing owner", "queued owner"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			memory := recoveryFixture()
			switch scenario {
			case "job version":
				memory.candidate.JobVersion = math.MaxInt64
			case "plan version":
				memory.candidate.ImportVersion = 0
			case "attempt":
				memory.candidate.Attempt = 0
			case "execution":
				memory.candidate.ExecutionNo = 0
			case "missing start":
				memory.candidate.StartedAtMS = nil
			case "missing deadline":
				memory.candidate.DeadlineAtMS = 0
			case "missing owner":
				memory.candidate.WorkerID = ""
			case "queued owner":
				memory.candidate.JobState = "QUEUED"
				memory.candidate.LeaseUntilMS = 0
			}
			memory.current = memory.candidate
			err := NewRecovery(memory, func() time.Time { return time.UnixMilli(1000) }).Recover(t.Context())
			if !errors.Is(err, model.ErrInvalid) || len(memory.changes) != 0 {
				t.Fatalf("invalid budget=%#v error=%v", memory.changes, err)
			}
		})
	}
}

func TestRecoveryTimeoutWinsExhaustedAttemptsAndOverflow(t *testing.T) {
	t.Parallel()
	for _, now := range []int64{1000, math.MaxInt64 - 119999} {
		memory := recoveryFixture()
		memory.candidate.Attempt = memory.candidate.MaxAttempts
		memory.candidate.DeadlineAtMS = now + 1
		memory.current = memory.candidate
		if err := NewRecovery(memory, func() time.Time { return time.UnixMilli(now) }).Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
		if len(memory.changes) != 1 || memory.changes[0].Code != "EMULATIONSTATION_EXECUTION_TIMEOUT" {
			t.Fatalf("timeout=%#v", memory.changes)
		}
	}
}

func assertRecoveryEligibility(t *testing.T, scenario string) {
	t.Helper()
	memory := recoveryFixture()
	want := ""
	switch scenario {
	case "unexpired":
		memory.candidate.LeaseUntilMS = 1001
	case "queued fresh", "queued deadline", "queued attempts":
		memory.candidate.JobState = "QUEUED"
		memory.candidate.WorkerID = ""
		memory.candidate.LeaseUntilMS = 0
		if scenario == "queued deadline" {
			memory.candidate.DeadlineAtMS = 1000
			want = "FAILED"
		}
		if scenario == "queued attempts" {
			memory.candidate.Attempt = memory.candidate.MaxAttempts
			want = "FAILED"
		}
	case "cancel expired", "cancel live":
		memory.candidate.JobState = "CANCEL_REQUESTED"
		memory.candidate.ImportState = "CANCEL_REQUESTED"
		want = "CANCELLED"
		if scenario == "cancel live" {
			memory.candidate.LeaseUntilMS = 1001
			want = ""
		}
	case "separate start pointer":
		want = "QUEUED"
	}
	memory.current = memory.candidate
	if scenario == "separate start pointer" {
		memory.current.StartedAtMS = new(*memory.candidate.StartedAtMS)
	}
	if err := NewRecovery(memory, func() time.Time { return time.UnixMilli(1000) }).Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if want == "" {
		if len(memory.changes) != 0 {
			t.Fatalf("ineligible changes=%#v", memory.changes)
		}
		return
	}
	if len(memory.changes) != 1 || memory.changes[0].JobState != want {
		t.Fatalf("recovery=%#v", memory.changes)
	}
}
