package emulationstationimport

import (
	"reflect"
	"testing"
	"time"

	application "retrom/internal/service/emulationstationimport"
)

func TestScanCancellationRollsBackCASWritesResponseAndCommitFailures(t *testing.T) {
	t.Parallel()
	for _, running := range []bool{false, true} {
		for _, phase := range []string{"callback"} {
			t.Run(map[bool]string{false: "queued/", true: "running/"}[running]+phase, func(t *testing.T) {
				t.Parallel()
				db, summary, job := stagedCancellation(t, running)
				before := planRows(t, db)
				service := application.NewWorkflowControl(workflowFaultRepository{WorkflowControl: NewWorkflowControl(db, nil), phase: phase}, nil, func() time.Time { return time.UnixMilli(1001) })
				result, pending, err := service.CancelJob(t.Context(), application.JobCancellationRequest{JobID: job.JobID, Kind: job.Kind, ScopeID: summary.ID, ExpectedVersion: job.JobVersion, Reason: "Stop", ActorID: "actor"})
				if err == nil || pending || result.JobID != "" {
					t.Fatalf("partial response=%#v pending=%v cause=%v", result, pending, err)
				}
				assertWorkflowFault(t, "cancel", phase, err)
				if !reflect.DeepEqual(before, planRows(t, db)) {
					t.Fatal("scan cancellation failure left durable writes")
				}
			})
		}
	}
}
