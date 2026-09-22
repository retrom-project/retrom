package payloadrelease

import (
	"errors"
	"testing"
)

func TestLifecyclePolicyChecksTerminalOwnersAndReleaseBinding(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		owner LifecycleOwner
		valid bool
	}{
		{"active retained", LifecycleOwner{Owner: Owner{Scope: Scope{Type: ScopeImportItem, ID: "item"}, State: "REVIEW_PENDING", PayloadState: "RETAINED"}}, true},
		{"terminal retained", LifecycleOwner{Owner: Owner{Scope: Scope{Type: ScopeImportItem, ID: "item"}, State: "PUBLISHED", PayloadState: "RETAINED"}}, false},
		{"retryable source retained", LifecycleOwner{Owner: Owner{Scope: Scope{Type: ScopeSourceImportItem, ID: "item"}, State: "READ_FAILED", Retryable: true, PayloadState: "RETAINED"}}, true},
		{"final source retained", LifecycleOwner{Owner: Owner{Scope: Scope{Type: ScopeSourceImportItem, ID: "item"}, State: "READ_FAILED", PayloadState: "RETAINED"}}, false},
		{"owned release", lifecycleReleased(Scope{Type: ScopeGame, ID: "game"}), true},
		{"public item release", lifecycleBoundSource(), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateLifecycleOwner(test.owner)
			if (err == nil) != test.valid || err != nil && !errors.Is(err, ErrLifecycleInvariant) {
				t.Fatalf("valid=%t error=%v", test.valid, err)
			}
		})
	}
}

func TestLifecyclePolicyRejectsForeignPublicBinding(t *testing.T) {
	t.Parallel()
	for _, mutate := range []func(*LifecycleOwner){
		func(owner *LifecycleOwner) { owner.ReleaseKind = "BLOB_GC" },
		func(owner *LifecycleOwner) { owner.ReleaseScope.ID = "other-item" },
		func(owner *LifecycleOwner) { owner.PublicReleaseJobID = "another-job" },
		func(owner *LifecycleOwner) { owner.ReleaseJobID = "" },
	} {
		owner := lifecycleBoundSource()
		mutate(&owner)
		if err := validateLifecycleOwner(owner); !errors.Is(err, ErrLifecycleInvariant) {
			t.Fatalf("foreign release allowed: %+v %v", owner, err)
		}
	}
}

func lifecycleReleased(scope Scope) LifecycleOwner {
	return LifecycleOwner{Owner: Owner{Scope: scope, State: "PUBLISHED", PayloadState: "RELEASING", ReleaseJobID: "release"}, ReleaseJobID: "release", ReleaseKind: "PAYLOAD_RELEASE", ReleaseScope: scope}
}

func lifecycleBoundSource() LifecycleOwner {
	owner := lifecycleReleased(Scope{Type: ScopeSourceImportItem, ID: "source"})
	owner.Owner.PublicID = "public"
	owner.ReleaseScope = Scope{Type: ScopeImportItem, ID: "public"}
	owner.PublicReleaseJobID = "release"
	return owner
}
