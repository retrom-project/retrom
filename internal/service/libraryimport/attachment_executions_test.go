package libraryimport

import "testing"

func TestAttachmentRecoveryHonorsLeaseDeadlineBudgetAndCancellation(t *testing.T) {
	expired, live := int64(20), int64(21)
	for _, test := range []struct {
		name, state, want, code string
		lease, deadline         *int64
		attempt                 int64
	}{
		{"live", "RUNNING", "", "", &live, &live, 1},
		{"interrupted", "RUNNING", "QUEUED", "ATTACHMENT_INTERRUPTED", &expired, &live, 1},
		{"missing lease", "RUNNING", "QUEUED", "ATTACHMENT_INTERRUPTED", nil, &live, 1},
		{"deadline", "RUNNING", "FAILED", "ATTACHMENT_EXECUTION_TIMEOUT", &live, &expired, 1},
		{"budget", "RUNNING", "FAILED", "ATTACHMENT_EXECUTION_EXHAUSTED", &expired, &live, 4},
		{"queued deadline", "QUEUED", "FAILED", "ATTACHMENT_EXECUTION_TIMEOUT", nil, &expired, 1},
		{"queued budget", "QUEUED", "FAILED", "ATTACHMENT_EXECUTION_EXHAUSTED", nil, &live, 4},
		{"queued", "QUEUED", "", "", nil, nil, 0},
		{"canceling live", "CANCEL_REQUESTED", "", "", &live, &live, 4},
		{"canceling dead", "CANCEL_REQUESTED", "CANCELLED", "", &expired, &expired, 4},
		{"cancelled", "CANCELLED", "CANCELLED", "", nil, nil, 0},
		{"succeeded", "SUCCEEDED", "", "", nil, nil, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			change, needed := attachmentRecovery(AttachmentExecution{
				State: test.state, LeaseMS: test.lease, DeadlineMS: test.deadline,
				Attempt: test.attempt, MaxAttempts: 4,
			}, 20)
			if needed != (test.want != "") || change.State != test.want || change.Code != test.code {
				t.Fatalf("change=%+v needed=%v", change, needed)
			}
			if change.State == "FAILED" && (change.Retryable == nil || !*change.Retryable) {
				t.Fatal("exhausted executions must allow an explicit new execution")
			}
		})
	}
}
