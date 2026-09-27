package cleanupjobs

import (
	"testing"

	jobs "retrom/internal/service/cleanupjobs"
	importcleanup "retrom/internal/service/libraryimport/payloadpolicy"
	uploadcleanup "retrom/internal/service/uploads/payloadpolicy"
)

func TestReleaseEffectPolicyRejectsActiveOrChangedOwners(t *testing.T) {
	scope := jobs.Scope{Type: jobs.ScopeImportItem, ID: "item"}
	unit := jobs.Execution{Work: jobs.Work{ID: "release", Scope: scope}, Input: jobs.Input{Inputs: jobs.ScopeInputs{ScopeVersion: 7}}}
	before := jobs.EffectOwner{
		Found: true,
		Owner: jobs.Owner{Scope: scope, State: "DISCARDED", PayloadState: "RELEASING", ReleaseJobID: "release", Version: 7},
	}
	if err := importcleanup.ValidateRelease(unit, before); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"REVIEW_PENDING", "SCRAPING", "FAILED_RETRYABLE"} {
		changed := before
		changed.Owner.State = state
		if err := importcleanup.ValidateRelease(unit, changed); jobs.WorkErrorCode(err) != "OWNER_CLEANUP_SCOPE_NOT_TERMINAL" {
			t.Fatalf("state=%s error=%v", state, err)
		}
	}
	changed := before
	changed.Owner.Version++
	if err := importcleanup.ValidateRelease(unit, changed); jobs.WorkErrorCode(err) != "OWNER_CLEANUP_SCOPE_VERSION_MISMATCH" {
		t.Fatalf("version error=%v", err)
	}
	changed = before
	changed.Owner.ReleaseJobID = "replacement"
	if err := importcleanup.ValidateRelease(unit, changed); jobs.WorkErrorCode(err) != "OWNER_CLEANUP_SCOPE_NOT_TERMINAL" {
		t.Fatalf("job error=%v", err)
	}
}

func TestReleaseEffectUploadEligibilityRequiresCompletedConsumptions(t *testing.T) {
	candidate := jobs.EffectUpload{
		ID: "file", SessionID: "upload", FileRecord: "blob",
		State: "COMPLETE", SessionState: "COMPLETE",
	}
	if !uploadcleanup.CanPurge(candidate.State, candidate.SessionState, candidate.ID, candidate.FileRecord, candidate.ActiveConsumptions) {
		t.Fatal("unreferenced completed file rejected")
	}
	for _, field := range []string{"consumption", "session", "file", "blob"} {
		t.Run(field, func(t *testing.T) {
			blocked := candidate
			switch field {
			case "consumption":
				blocked.ActiveConsumptions = 1
			case "session":
				blocked.SessionState = "FINALIZING"
			case "file":
				blocked.State = "UPLOADING"
			case "blob":
				blocked.FileRecord = ""
			}
			if uploadcleanup.CanPurge(blocked.State, blocked.SessionState, blocked.ID, blocked.FileRecord, blocked.ActiveConsumptions) {
				t.Fatalf("protected %s file eligible", field)
			}
		})
	}
}
