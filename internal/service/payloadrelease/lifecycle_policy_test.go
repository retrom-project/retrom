package payloadrelease

import (
	"errors"
	model "retrom/internal/model/payloadrelease"
	"testing"
)

func TestLifecyclePolicyChecksTerminalOwnersAndReleaseBinding(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		owner model.LifecycleOwner
		valid bool
	}{
		{"active retained", model.LifecycleOwner{Owner: model.Owner{Scope: model.Scope{Type: model.ScopeImportItem, ID: "item"}, State: "REVIEW_PENDING", PayloadState: "RETAINED"}}, true},
		{"terminal retained", model.LifecycleOwner{Owner: model.Owner{Scope: model.Scope{Type: model.ScopeImportItem, ID: "item"}, State: "PUBLISHED", PayloadState: "RETAINED"}}, false},
		{"retryable source retained", model.LifecycleOwner{Owner: model.Owner{Scope: model.Scope{Type: model.ScopePegasusImportItem, ID: "item"}, State: "READ_FAILED", Retryable: true, PayloadState: "RETAINED"}}, true},
		{"final source retained", model.LifecycleOwner{Owner: model.Owner{Scope: model.Scope{Type: model.ScopeEmulationStationImportItem, ID: "item"}, State: "READ_FAILED", PayloadState: "RETAINED"}}, false},
		{"owned release", lifecycleReleased(model.Scope{Type: model.ScopeGame, ID: "game"}), true},
		{"public item release", lifecycleBoundSource(), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateLifecycleOwner(test.owner)
			if (err == nil) != test.valid || err != nil && !errors.Is(err, model.ErrLifecycleInvariant) {
				t.Fatalf("valid=%t error=%v", test.valid, err)
			}
		})
	}
}

func TestLifecyclePolicyRejectsForeignPublicBinding(t *testing.T) {
	t.Parallel()
	for _, mutate := range []func(*model.LifecycleOwner){
		func(owner *model.LifecycleOwner) { owner.ReleaseKind = "BLOB_GC" },
		func(owner *model.LifecycleOwner) { owner.ReleaseScope.ID = "other-item" },
		func(owner *model.LifecycleOwner) { owner.PublicReleaseJobID = "another-job" },
		func(owner *model.LifecycleOwner) { owner.ReleaseJobID = "" },
	} {
		owner := lifecycleBoundSource()
		mutate(&owner)
		if err := validateLifecycleOwner(owner); !errors.Is(err, model.ErrLifecycleInvariant) {
			t.Fatalf("foreign release allowed: %+v %v", owner, err)
		}
	}
}

func lifecycleReleased(scope model.Scope) model.LifecycleOwner {
	return model.LifecycleOwner{Owner: model.Owner{Scope: scope, State: "PUBLISHED", PayloadState: "RELEASING", ReleaseJobID: "release"}, ReleaseJobID: "release", ReleaseKind: "PAYLOAD_RELEASE", ReleaseScope: scope}
}

func lifecycleBoundSource() model.LifecycleOwner {
	owner := lifecycleReleased(model.Scope{Type: model.ScopeEmulationStationImportItem, ID: "source"})
	owner.Owner.PublicID = "public"
	owner.ReleaseScope = model.Scope{Type: model.ScopeImportItem, ID: "public"}
	owner.PublicReleaseJobID = "release"
	return owner
}
