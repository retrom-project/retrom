package pegasusimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/pegasusimport"
)

type completionFake struct {
	before  model.ExecutionSnapshot
	counts  model.CompletionCounts
	saved   *model.CompletionChange
	failure error
}

func (fake *completionFake) CommitCompletion(_ context.Context, identity model.ExecutionIdentity, nowMS int64) error {
	if err := model.ValidateExecution(fake.before, identity, nowMS); err != nil {
		return err
	}
	if fake.before.Kind != "SERVER_PEGASUS_IMPORT" || fake.before.JobState != "RUNNING" {
		return model.ErrVersionConflict
	}
	if fake.failure != nil {
		return fake.failure
	}
	if fake.counts.Unfinished != 0 {
		return model.ErrVersionConflict
	}
	change := model.CompletionChange{
		Before:      fake.before,
		Counts:      fake.counts,
		ImportState: "COMPLETED",
		Retryable:   fake.counts.Failed > 0,
		NowMS:       nowMS,
	}
	if fake.counts.Blocked > 0 || fake.counts.Failed > 0 {
		change.ImportState = "PARTIAL_FAILURE"
	}
	fake.saved = &change
	return nil
}

func completionFixture() (*completionFake, model.ExecutionIdentity) {
	before := model.ExecutionSnapshot{
		JobID: "job", ImportID: "import", WorkerID: "owner", Kind: "SERVER_PEGASUS_IMPORT", JobState: "RUNNING", ImportState: "RUNNING",
		ExecutionNo: 1, Attempt: 1, JobVersion: 2, ImportVersion: 3, LeaseUntilMS: 50, DeadlineMS: 100,
	}
	return &completionFake{before: before}, model.ExecutionIdentity{JobID: "job", ImportID: "import", WorkerID: "owner", ExecutionNo: 1, Attempt: 1}
}

func TestCompletionRequiresCurrentWorkerAndNoPendingItems(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"success", "blocked", "failed", "worker", "pending", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			fake, identity := completionFixture()
			switch name {
			case "blocked":
				fake.counts.Blocked = 1
			case "failed":
				fake.counts.Failed = 1
			case "worker":
				identity.WorkerID = "previous"
			case "pending":
				fake.counts.Unfinished = 1
			case "cancelled":
				fake.before.JobState, fake.before.ImportState = "CANCEL_REQUESTED", "CANCEL_REQUESTED"
			}
			err := NewCompletion(fake, func() time.Time { return time.UnixMilli(10) }).Finish(t.Context(), identity)
			valid := name == "success" || name == "blocked" || name == "failed"
			if valid != (err == nil) || valid != (fake.saved != nil) {
				t.Fatalf("saved=%+v err=%v", fake.saved, err)
			}
			if valid {
				state := "COMPLETED"
				if name != "success" {
					state = "PARTIAL_FAILURE"
				}
				if fake.saved.ImportState != state || fake.saved.Retryable != (name == "failed") {
					t.Fatalf("completion=%+v", fake.saved)
				}
			}
		})
	}
}

func TestCompletionCountFailureRetainsCause(t *testing.T) {
	t.Parallel()
	fake, identity := completionFixture()
	cause := errors.New("counts unavailable")
	fake.failure = cause
	err := NewCompletion(fake, func() time.Time { return time.UnixMilli(10) }).Finish(t.Context(), identity)
	if !errors.Is(err, cause) || fake.saved != nil {
		t.Fatalf("completion error=%v saved=%+v", err, fake.saved)
	}
}
