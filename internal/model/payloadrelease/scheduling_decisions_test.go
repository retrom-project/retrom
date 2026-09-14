package payloadrelease

import (
	"errors"
	"math"
	"testing"
)

func TestDecideOwnerReleaseSharesReplayAndCreationRules(t *testing.T) {
	t.Parallel()
	ref := Scope{Type: ScopeImportItem, ID: "item"}
	for _, test := range []struct {
		name        string
		owner       Owner
		wantID      string
		wantVersion int64
		wantCreate  bool
		wantError   bool
	}{
		{name: "retained", owner: Owner{Scope: ref, PayloadState: "RETAINED", Version: 3}, wantVersion: 4, wantCreate: true},
		{name: "releasing", owner: Owner{Scope: ref, PayloadState: "RELEASING", ReleaseJobID: "job"}, wantID: "job"},
		{name: "released", owner: Owner{Scope: ref, PayloadState: "RELEASED", ReleaseJobID: "job"}, wantID: "job"},
		{name: "failed", owner: Owner{Scope: ref, PayloadState: "FAILED", ReleaseJobID: "job"}, wantID: "job"},
		{name: "missing replay job", owner: Owner{Scope: ref, PayloadState: "RELEASED"}, wantError: true},
		{name: "overflow", owner: Owner{Scope: ref, PayloadState: "RETAINED", Version: math.MaxInt64}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			decision, err := DecideOwnerRelease(test.owner, ReasonImportPublished)
			if test.wantError {
				if !errors.Is(err, ErrScopeInvalid) {
					t.Fatalf("error = %v, want ErrScopeInvalid", err)
				}
				return
			}
			if err != nil || decision.ExistingJobID != test.wantID || decision.ScopeVersion != test.wantVersion || decision.Schedule != test.wantCreate {
				t.Fatalf("decision=%#v error=%v", decision, err)
			}
		})
	}
}

func TestDecideSourceLinkRequiresRetainedSourceAndOrdinaryRelease(t *testing.T) {
	t.Parallel()
	source := Owner{PayloadState: "RETAINED", Version: 2}
	ordinary := Owner{PayloadState: "RELEASING", ReleaseJobID: "job"}
	decision, err := DecideSourceLink(source, ordinary)
	if err != nil || decision.ExistingJobID != "job" {
		t.Fatalf("decision=%#v error=%v", decision, err)
	}
	if _, err := DecideSourceLink(Owner{PayloadState: "RETAINED", Version: math.MaxInt64}, ordinary); !errors.Is(err, ErrScopeInvalid) {
		t.Fatalf("overflow source error=%v", err)
	}
}
