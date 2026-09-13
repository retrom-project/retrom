package libraryimport

import "testing"

func TestImportExecutionAuthorityTracksIdentityLeaseAndTerminalPolicy(t *testing.T) {
	expected := QueuedImportExecution{
		ImportID: "import", JobID: "job", WorkerID: "worker", ActorUserID: "actor",
		ExecutionNo: 3, Attempt: 2, StartedAtMS: 10, DeadlineMS: 100,
	}
	for _, test := range []struct {
		name    string
		mutate  func(*CreationQueuedSnapshot)
		allowed bool
	}{
		{"current", func(*CreationQueuedSnapshot) {}, true},
		{
			"cancellation retains owner",
			func(value *CreationQueuedSnapshot) { value.JobState = "CANCEL_REQUESTED" },
			true,
		},
		{"replaced worker", func(value *CreationQueuedSnapshot) { value.Execution.WorkerID = "replacement" }, false},
		{"new execution", func(value *CreationQueuedSnapshot) { value.Execution.ExecutionNo++ }, false},
		{"new attempt", func(value *CreationQueuedSnapshot) { value.Execution.Attempt++ }, false},
		{"renewed deadline", func(value *CreationQueuedSnapshot) { value.Execution.DeadlineMS++ }, false},
		{"expired lease", func(value *CreationQueuedSnapshot) { value.LeaseUntilMS = 50 }, false},
		{"finished", func(value *CreationQueuedSnapshot) { value.JobState = "SUCCEEDED" }, false},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				current := CreationQueuedSnapshot{Execution: expected, JobState: "RUNNING", JobVersion: 1, LeaseUntilMS: 60}
				test.mutate(&current)
				if actual := ImportExecutionCurrent(expected, current, 50); actual != test.allowed {
					t.Fatalf("execution authority=%t, want %t", actual, test.allowed)
				}
			},
		)
	}
}

func TestCreationProgressUsesSharedAggregatePriority(t *testing.T) {
	for _, test := range []struct {
		provider            string
		remaining, rejected int64
		state               string
		running, pending    int64
		completed           bool
	}{
		{"NONE", 0, 0, "COMPLETED", 0, 0, true},
		{"NONE", 0, 1, "PARTIAL_FAILURE", 0, 0, false},
		{"NONE", 1, 0, "REVIEW_PENDING", 0, 1, false},
		{"NONE", 1, 1, "PARTIAL_FAILURE", 0, 1, false},
		{"HASHEOUS", 1, 1, "RUNNING", 1, 0, false},
	} {
		result, running, pending, err := creationProgress(test.provider, test.remaining, test.rejected, 100)
		if err != nil || result.State != test.state || running != test.running || pending != test.pending ||
			(result.CompletedAtMS != nil) != test.completed {
			t.Fatalf(
				"creation progress %+v: projection=%+v running=%d pending=%d error=%v",
				test,
				result,
				running,
				pending,
				err,
			)
		}
	}
}
