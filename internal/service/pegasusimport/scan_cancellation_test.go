package pegasusimport

import (
	"context"
	"errors"
	model "retrom/internal/model/pegasusimport"
	"testing"
	"time"
)

func (memory *workflowMemory) CurrentJob(context.Context, string) (model.WorkflowSnapshot, error) {
	return memory.before, memory.err
}

func TestScanCancellationUsesOriginalJobVersionInDomainTransaction(t *testing.T) {
	t.Parallel()
	memory := workflowFixture()
	memory.before.Summary.State = "SCANNING"
	memory.before.Summary.ImportJobID = nil
	memory.before.Summary.ScanJobID = "scan"
	memory.before.JobState = "RUNNING"
	service := NewWorkflowControl(memory, func() time.Time { return time.UnixMilli(10) })
	if result, pending, err := service.CancelJob(
		t.Context(),
		JobCancellationRequest{
			JobID:           "scan",
			ScopeID:         "import",
			Kind:            "SERVER_PEGASUS_SCAN",
			ExpectedVersion: 4,
			Reason:          "Stop",
			ActorID:         "actor",
		},
	); !errors.Is(
		err,
		model.ErrVersionConflict,
	) || pending || result.JobID != "" || memory.cancellation != nil {
		t.Fatalf("used plan ETag instead of job ETag: %#v %v %v", result, pending, err)
	}
	if result, pending, err := service.CancelJob(
		t.Context(),
		JobCancellationRequest{
			JobID:           "scan",
			ScopeID:         "import",
			Kind:            "SERVER_PEGASUS_SCAN",
			ExpectedVersion: 3,
			Reason:          "Stop",
			ActorID:         "actor",
		},
	); err != nil || !pending || result.State != "CANCEL_REQUESTED" {
		t.Fatalf("correct job ETag rejected: %#v %v %v", result, pending, err)
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
				JobID:           "scan",
				ScopeID:         "import",
				Kind:            "SERVER_PEGASUS_SCAN",
				ExpectedVersion: 3,
				Reason:          "Stop",
				ActorID:         "actor",
			},
		)
		if !errors.Is(err, cause) || result.JobID != "" || pending {
			t.Fatalf("partial cancel response: %#v %v %v", result, pending, err)
		}
	}
}
