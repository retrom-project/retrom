package libraryimport

import (
	"context"
	"errors"
	model "retrom/internal/model/libraryimport"
	"testing"

	"retrom/internal/model/importprogress"
)

func workerPolicySnapshot() model.ImportWorkerSnapshot {
	return model.ImportWorkerSnapshot{
		Creation: model.CreationQueuedSnapshot{
			JobState:    "RUNNING",
			ImportState: "RUNNING",
			MaxAttempts: 3,
			Execution: model.QueuedImportExecution{
				JobID:       "job",
				ImportID:    "import",
				WorkerID:    "worker",
				ExecutionNo: 2,
				Attempt:     1,
				StartedAtMS: 10,
				DeadlineMS:  100000,
			},
			LeaseUntilMS: 2000,
		},
		StartedAtMS:  importMoment(10),
		DeadlineAtMS: importMoment(100000),
		LeaseUntilMS: importMoment(2000),
	}
}

func TestImportWorkerFailurePolicy(t *testing.T) {
	cases := []struct {
		name, state       string
		cause             error
		attempt, deadline int64
		want              string
		release           bool
		code              string
	}{
		{"transient", "RUNNING", errors.New("storage failed"), 1, 100000, "QUEUED", false, "IMPORT_GROUP_FAILED"},
		{"permanent", "RUNNING", model.ErrInvalid, 1, 100000, "FAILED", true, "IMPORT_INPUT_INVALID"},
		{"exhausted", "RUNNING", errors.New("storage failed"), 3, 100000, "FAILED", false, "IMPORT_GROUP_FAILED"},
		{
			"retry exceeds budget",
			"RUNNING",
			errors.New("storage failed"),
			1,
			1500,
			"FAILED",
			false,
			"IMPORT_GROUP_FAILED",
		},
		{
			"deadline before close",
			"RUNNING",
			ErrImportWorkerClosed,
			1,
			1000,
			"FAILED",
			false,
			"IMPORT_GROUP_EXECUTION_TIMEOUT",
		},
		{"close", "RUNNING", ErrImportWorkerClosed, 1, 100000, "QUEUED", false, "IMPORT_GROUP_INTERRUPTED"},
		{"request cancellation", "RUNNING", context.Canceled, 1, 100000, "QUEUED", false, "IMPORT_GROUP_INTERRUPTED"},
		{"persisted cancellation", "CANCEL_REQUESTED", context.DeadlineExceeded, 1, 1000, "CANCELLED", true, ""},
	}
	for _, test := range cases {
		t.Run(
			test.name,
			func(t *testing.T) {
				before := workerPolicySnapshot()
				before.Creation.JobState = test.state
				before.Creation.Execution.Attempt = test.attempt
				before.Creation.Execution.DeadlineMS = test.deadline
				before.DeadlineAtMS = importMoment(test.deadline)
				result, release, err := importFailureProjection(before, test.cause, 1000)
				if err != nil || result.Job.State != test.want || result.Parent.State != test.want || release != test.release {
					t.Fatalf("failure transition=%+v release=%t error=%v", result, release, err)
				}
				if result.Job.Execution.WorkerID != "" || result.Job.LeaseUntilMS != nil || result.Job.HeartbeatAtMS != nil {
					t.Fatalf("terminal/retry retained worker: %+v", result.Job)
				}
				if result.Job.Execution.StartedAtMS != 10 || result.Job.Execution.DeadlineMS != test.deadline ||
					result.Job.Execution.ExecutionNo != 2 {
					t.Fatalf("failure reset budget: %+v", result.Job.Execution)
				}
				if test.code != "" && (result.Job.ErrorCode == nil || *result.Job.ErrorCode != test.code) {
					t.Fatalf("failure code=%v want=%s", result.Job.ErrorCode, test.code)
				}
			},
		)
	}
}

func TestImportWorkerRecoveryPolicy(t *testing.T) {
	cases := []struct {
		name    string
		change  func(*model.ImportWorkerSnapshot)
		want    string
		release bool
	}{
		{"expired lease", func(v *model.ImportWorkerSnapshot) { v.Creation.LeaseUntilMS = 1000 }, "QUEUED", false},
		{"expired budget", func(v *model.ImportWorkerSnapshot) { v.DeadlineAtMS = importMoment(1000) }, "FAILED", false},
		{"cancel requested", func(v *model.ImportWorkerSnapshot) {
			v.Creation.JobState = "CANCEL_REQUESTED"
			v.Creation.LeaseUntilMS = 1000
		}, "CANCELLED", true},
		{"committed review", func(v *model.ImportWorkerSnapshot) {
			v.ItemCount = 1
			v.Counts = importprogress.Counts{ReviewPending: 1}
			v.Creation.LeaseUntilMS = 1000
		}, "SUCCEEDED", true},
		{"committed rejected file", func(v *model.ImportWorkerSnapshot) {
			v.ResolvedFiles = 1
			v.Counts = importprogress.Counts{Rejected: 1}
			v.Creation.LeaseUntilMS = 1000
		}, "SUCCEEDED", true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			before := workerPolicySnapshot()
			test.change(&before)
			if !importRecoveryRequired(before, 1000) {
				t.Fatal("expired worker not recoverable")
			}
			result, release, err := importRecoveryProjection(before, 1000)
			if err != nil || result.Job.State != test.want || release != test.release {
				t.Fatalf("recovery=%+v release=%t error=%v", result, release, err)
			}
		})
	}
	if importRecoveryRequired(workerPolicySnapshot(), 1000) {
		t.Fatal("live worker recoverable")
	}
}

func TestImportWorkerClaimRequiresNoCommittedOutcomes(t *testing.T) {
	before := workerPolicySnapshot()
	before.Creation.JobState = "QUEUED"
	before.Creation.ImportState = "QUEUED"
	if !importClaimAvailable(before, 1000) {
		t.Fatal("new queued work not claimable")
	}
	before.ResolvedFiles = 1
	if importClaimAvailable(before, 1000) {
		t.Fatal("resolved file was claimable")
	}
	before.ResolvedFiles = 0
	before.ItemCount = 1
	if importClaimAvailable(before, 1000) {
		t.Fatal("committed item was claimable")
	}
}
