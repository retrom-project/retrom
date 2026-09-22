package sourceimport

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	library "retrom/internal/service/libraryimport"
)

func recoverySnapshot() RecoverySnapshot {
	return RecoverySnapshot{JobID: "job", ImportID: "import", Kind: "IMPORT_RECEIVE", JobState: "RUNNING", ImportState: "RUNNING", WorkerID: "lost", JobVersion: 2, ImportVersion: 3, ExecutionNo: 1, Attempt: 1, MaxAttempts: 4, LeaseUntilMS: 5, DeadlineMS: 100}
}

func TestRecoveryPolicyPreservesOriginalExecutionAndPrioritizesCancellation(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"retry", "timeout", "exhausted", "cancel", "scan"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			before := recoverySnapshot()
			state, code := "QUEUED", ""
			switch mode {
			case "timeout":
				before.DeadlineMS = 10
				state, code = "FAILED", "SOURCE_EXECUTION_TIMEOUT"
			case "exhausted":
				before.Attempt = 4
				state, code = "FAILED", "SOURCE_WORKER_ATTEMPTS_EXHAUSTED"
			case "cancel":
				before.JobState = "CANCEL_REQUESTED"
				before.ImportState = "CANCEL_REQUESTED"
				before.DeadlineMS = 1
				before.Attempt = 4
				state = "CANCELLED"
			case "scan":
				before.Kind = "IMPORT_SCAN"
				before.ImportState = "SCANNING"
			}
			change, err := planRecovery(before, 10)
			if err != nil || change.Before != before || change.JobState != state || change.Code != code {
				t.Fatalf("recovery=%#v error=%v", change, err)
			}
			if mode == "scan" && change.ImportState != "SCANNING" {
				t.Fatalf("scan became %s", change.ImportState)
			}
		})
	}
}

func TestRecoveryRejectsInvalidOrLiveExecution(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"lease", "deadline", "attempt", "state", "version"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			before := recoverySnapshot()
			switch field {
			case "lease":
				before.LeaseUntilMS = 11
			case "deadline":
				before.DeadlineMS = 0
			case "attempt":
				before.Attempt = 0
			case "state":
				before.ImportState = "COMPLETED"
			case "version":
				before.JobVersion = 0
			}
			if _, err := planRecovery(before, 10); err == nil {
				t.Fatalf("accepted invalid %s", field)
			}
		})
	}
}

type recoveryFake struct {
	before    RecoverySnapshot
	current   RecoverySnapshot
	failure   error
	applied   int
	limit     int
	reviews   []ReviewHandoffSnapshot
	completed int
	metadata  *handoffMemory
}

func (fake *recoveryFake) ExpiredExecutions(_ context.Context, _ int64, limit int) ([]RecoverySnapshot, error) {
	fake.limit = limit
	return []RecoverySnapshot{fake.before}, nil
}

func (fake *recoveryFake) WithRecovery(_ context.Context, work func(RecoveryScope) error) error {
	return work(RecoveryScope{Payload: emptyPayloadScope(), Records: fake, Metadata: fake.metadata})
}

func (fake *recoveryFake) Current(context.Context, string) (RecoverySnapshot, error) {
	return fake.current, fake.failure
}

func (fake *recoveryFake) Reviews(context.Context, string, int) ([]ReviewHandoffSnapshot, error) {
	return fake.reviews, nil
}

func (fake *recoveryFake) CompleteReview(context.Context, RecoveryReviewChange) error {
	fake.completed++
	fake.current.ImportVersion++
	return nil
}

func (fake *recoveryFake) Apply(context.Context, RecoveryChange) error { fake.applied++; return nil }

func TestRecoverySkipsStaleCandidateAndPreservesRepositoryFailure(t *testing.T) {
	t.Parallel()
	original := errors.New("storage unavailable")
	for _, failed := range []bool{false, true} {
		fake := &recoveryFake{before: recoverySnapshot(), current: recoverySnapshot()}
		fake.current.Attempt++
		if failed {
			fake.failure = original
		}
		service := NewRecovery(fake, nil, func() time.Time { return time.UnixMilli(10) })
		err := service.Recover(t.Context())
		if errors.Is(err, original) != failed || fake.applied != 0 || fake.limit != 100 {
			t.Fatalf("failed=%v error=%v fake=%#v", failed, err, fake)
		}
	}
}

func TestRecoveryBoundsReviewReconciliationBeforeClosingExecution(t *testing.T) {
	t.Parallel()
	for _, count := range []int{100, 101} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			t.Parallel()
			fake := &recoveryFake{before: recoverySnapshot(), current: recoverySnapshot(), metadata: readyHandoffMemory()}
			for i := range count {
				review := readyHandoffMemory().before
				review.Identity.ItemID = fmt.Sprint(i)
				review.Identity.ExecutionNo, review.Identity.Attempt = 1, 1
				review.ImportVersion = fake.current.ImportVersion
				fake.reviews = append(fake.reviews, review)
			}
			now := func() time.Time { return time.UnixMilli(10) }
			if err := NewRecovery(fake, library.NewMetadataSeeder(nil, now), now).Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			expectedApplies := 0
			if count == 100 {
				expectedApplies = 1
			}
			if fake.completed != 100 || fake.metadata.seeds != 100 || fake.applied != expectedApplies {
				t.Fatalf("count=%d completed=%d seeds=%d applies=%d", count, fake.completed, fake.metadata.seeds, fake.applied)
			}
		})
	}
}
