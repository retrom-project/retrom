package emulationstationimport

import (
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

func scanWorkflowFixture() (*workflowMemory, *startSources) {
	memory, sources := workflowFixture()
	memory.current.Summary.ImportJobID = nil
	memory.current.Summary.ScanJobID = "scan-job"
	memory.current.Summary.State = "SCANNING"
	memory.current.JobState = "RUNNING"
	return memory, sources
}

func TestCancelScanJobChecksOriginalJobVersionAndScope(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"valid", "stale job version", "wrong job", "wrong kind", "wrong scope"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			memory, sources := scanWorkflowFixture()
			request := JobCancellationRequest{JobID: "scan-job", Kind: "SERVER_EMULATIONSTATION_SCAN", ScopeID: memory.current.Summary.ID, ExpectedVersion: 3, Reason: " Stop ", ActorID: "editor"}
			switch scenario {
			case "stale job version":
				request.ExpectedVersion = 4
			case "wrong job":
				request.JobID = "replacement"
			case "wrong kind":
				request.Kind = "SERVER_EMULATIONSTATION_IMPORT"
			case "wrong scope":
				request.ScopeID = "unrelated"
			}
			result, pending, err := NewWorkflowControl(memory, sources, func() time.Time { return time.UnixMilli(1000) }).CancelJob(t.Context(), request)
			assertScanJobCancellation(t, memory, result, pending, err, scenario)
		})
	}
}

func TestCancelScanJobCommitFailureDiscardsResponse(t *testing.T) {
	t.Parallel()
	memory, sources := scanWorkflowFixture()
	memory.stage = "commit"
	memory.failure = errors.New("commit rejected")
	result, pending, err := NewWorkflowControl(memory, sources, func() time.Time { return time.UnixMilli(1000) }).CancelJob(t.Context(), JobCancellationRequest{JobID: "scan-job", Kind: "SERVER_EMULATIONSTATION_SCAN", ScopeID: memory.current.Summary.ID, ExpectedVersion: 3, Reason: "Stop", ActorID: "editor"})
	if !errors.Is(err, memory.failure) || pending || result.JobID != "" {
		t.Fatalf("commit=%#v pending=%v err=%v", result, pending, err)
	}
}

func assertScanJobCancellation(t *testing.T, memory *workflowMemory, result JobCancellationResult, pending bool, err error, scenario string) {
	t.Helper()
	if scenario == "valid" {
		if err != nil || !pending || result.JobID != "scan-job" || memory.cancel == nil || memory.cancel.Reason != "Stop" || memory.cancel.ActorID != "editor" {
			t.Fatalf("cancel=%#v pending=%v err=%v plan=%#v", result, pending, err, memory.cancel)
		}
	} else if err == nil || pending || result.JobID != "" || memory.cancel != nil {
		t.Fatalf("invalid cancel=%#v pending=%v err=%v", result, pending, err)
	}
	if scenario == "stale job version" && !errors.Is(err, model.ErrVersionConflict) {
		t.Fatalf("ETag error=%v", err)
	}
}
