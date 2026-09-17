package payloadrelease

import (
	model "retrom/internal/model/payloadrelease"
	"testing"
)

func TestReleaseEffectPolicyRejectsActiveOrChangedOwners(t *testing.T) {
	scope := model.Scope{Type: model.ScopeImportItem, ID: "item"}
	unit := model.Execution{Work: model.Work{ID: "release", Scope: scope}, Input: model.Input{Inputs: model.ScopeInputs{ScopeVersion: 7}}}
	before := model.EffectOwner{
		Found:       true,
		Owner: model.Owner{Scope: scope, State: "DISCARDED", PayloadState: "RELEASING", ReleaseJobID: "release", Version: 7},
	}
	if err := validateEffectRoot(unit, before); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"REVIEW_PENDING", "SCRAPING", "FAILED_RETRYABLE"} {
		changed := before
		changed.Owner.State = state
		if err := validateEffectRoot(unit, changed); WorkErrorCode(err) != "PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL" {
			t.Fatalf("state=%s error=%v", state, err)
		}
	}
	changed := before
	changed.Owner.Version++
	if err := validateEffectRoot(unit, changed); WorkErrorCode(err) != "PAYLOAD_RELEASE_SCOPE_VERSION_MISMATCH" {
		t.Fatalf("version error=%v", err)
	}
	changed = before
	changed.Owner.ReleaseJobID = "replacement"
	if err := validateEffectRoot(unit, changed); WorkErrorCode(err) != "PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL" {
		t.Fatalf("job error=%v", err)
	}
}

func TestReleaseEffectUploadEligibilityRequiresEveryReferenceGone(t *testing.T) {
	candidate := model.EffectUpload{ID: "file", SessionID: "upload", BlobID: "blob", State: "COMPLETE", SessionState: "COMPLETE"}
	if !eligibleEffectUpload(candidate) {
		t.Fatal("unreferenced completed file rejected")
	}
	for _, field := range []string{"consumption", "reference", "session", "file", "blob"} {
		t.Run(field, func(t *testing.T) {
			blocked := candidate
			switch field {
			case "consumption":
				blocked.ActiveConsumptions = 1
			case "reference":
				blocked.DomainReferences = 1
			case "session":
				blocked.SessionState = "FINALIZING"
			case "file":
				blocked.State = "UPLOADING"
			case "blob":
				blocked.BlobID = ""
			}
			if eligibleEffectUpload(blocked) {
				t.Fatalf("protected %s file eligible", field)
			}
		})
	}
}
