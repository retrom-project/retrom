package payloadrelease

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestBuildScheduledJobIsDeterministicForExplicitIdentities(t *testing.T) {
	t.Parallel()
	request := ScheduleRequest{
		Scope: Scope{Type: ScopeImportItem, ID: "item"}, ScopeVersion: 3,
		Reason: ReasonImportPublished, NowMS: 42,
	}
	job, err := BuildScheduledJob(request, "job", "execution")
	if err != nil {
		t.Fatal(err)
	}
	var input Input
	if err := json.Unmarshal([]byte(job.InputJSON), &input); err != nil {
		t.Fatal(err)
	}
	if job.ID != "job" || job.Scope != request.Scope || input.ExecutionID != "execution" ||
		input.Inputs.ScopeVersion != request.ScopeVersion || input.Inputs.Reason != request.Reason ||
		job.InputDigest == "" || job.DedupeKey == "" {
		t.Fatalf("job = %#v, input = %#v", job, input)
	}
}

func TestBuildScheduledJobRejectsMissingIdentity(t *testing.T) {
	t.Parallel()
	_, err := BuildScheduledJob(ScheduleRequest{
		Scope: Scope{Type: ScopeGame, ID: "game"}, ScopeVersion: 1,
		Reason: ReasonGameDeleted,
	}, "", "execution")
	if !errors.Is(err, ErrScheduleIDInvalid) {
		t.Fatalf("error = %v, want ErrScheduleIDInvalid", err)
	}
}
