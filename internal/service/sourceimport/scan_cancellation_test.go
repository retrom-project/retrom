package sourceimport

import (
	"context"
	"errors"
	"testing"
	"time"
)

func (memory *workflowMemory) CurrentJob(context.Context, string) (WorkflowSnapshot, error) {
	return memory.before, memory.err
}

func TestScanCancellationUsesLatestExecutionAndIsIdempotent(t *testing.T) {
	memory := workflowFixture()
	memory.before.Summary.State = "SCANNING"
	memory.before.Summary.ImportJobID = nil
	memory.before.Summary.ScanJobID = "scan"
	memory.before.Summary.Version = 99
	memory.before.JobState = "RUNNING"
	memory.before.JobVersion = 42
	memory.before.Execution = 7
	service := NewWorkflowControl(memory, func() time.Time { return time.UnixMilli(10) })
	command := JobCancellationRequest{JobID: "scan", ScopeID: "import", Kind: "IMPORT_SCAN", Reason: "Stop", ActorID: "actor"}
	first, pending, err := service.CancelJob(t.Context(), command)
	if err != nil || !pending || first.State != "CANCEL_REQUESTED" || first.Version != 43 || first.ExecutionNo != 7 {
		t.Fatalf("latest execution cancellation: %#v %v %v", first, pending, err)
	}
	memory.cancellation = nil
	second, pending, err := service.CancelJob(t.Context(), command)
	if err != nil || !pending || second != first || memory.cancellation != nil {
		t.Fatalf("repeat cancellation changed state: %#v %v %v", second, pending, err)
	}
}

func TestScanCancellationPreservesReadAndCommitFailures(t *testing.T) {
	t.Parallel()
	for _, commit := range []bool{false, true} {
		memory := workflowFixture()
		memory.before.Summary.State = "SCANNING"
		memory.before.Summary.ImportJobID = nil
		memory.before.Summary.ScanJobID = "scan"
		memory.before.JobState = "QUEUED"
		cause := errors.New("scan cancellation transaction failed")
		if commit {
			memory.commitErr = cause
		} else {
			memory.err = cause
		}
		service := NewWorkflowControl(memory, func() time.Time { return time.UnixMilli(10) })
		result, pending, err := service.CancelJob(
			t.Context(),
			JobCancellationRequest{
				JobID:   "scan",
				ScopeID: "import",
				Kind:    "IMPORT_SCAN",
				Reason:  "Stop",
				ActorID: "actor",
			},
		)
		if !errors.Is(err, cause) || result.JobID != "" || pending {
			t.Fatalf("partial cancel response: %#v %v %v", result, pending, err)
		}
	}
}
