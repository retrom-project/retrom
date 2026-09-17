package runtimeprovider

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"retrom/internal/capability/runtime/runtimebundle"
	model "retrom/internal/model/runtimeprovider"
)

func TestRejectedVersionDoesNotWriteProjection(t *testing.T) {
	for _, test := range []struct {
		version, digest string
		expected        error
	}{
		{"0.9.0", "b", model.ErrProviderDowngrade},
		{"1.0.0", "b", model.ErrProviderVersionRebuilt},
	} {
		repo := &reconciliationRepository{
			reconcileErr: test.expected,
		}
		err := New(repo).Reconcile(
			t.Context(), businessCandidate(test.version, test.digest),
			time.UnixMilli(1000),
		)
		if !errors.Is(err, test.expected) {
			t.Fatalf("rejected upgrade: %v", err)
		}
	}
}

func TestUnchangedProjectionDoesNotInterruptSessions(t *testing.T) {
	repo := &reconciliationRepository{}
	candidate := businessCandidate("1.0.0", "a")
	if err := New(repo).Reconcile(
		t.Context(), candidate, time.UnixMilli(1000),
	); err != nil {
		t.Fatal(err)
	}
	if repo.reconcileCalled != 1 {
		t.Fatalf("reconcile not called: %d", repo.reconcileCalled)
	}
}

func TestUnreadableCheckpointRejectsBeforeTermination(t *testing.T) {
	repo := &reconciliationRepository{
		reconcileErr: model.ErrProviderCheckpointUnreadable,
	}
	candidate := businessCandidate("1.1.0", "b")
	candidate.Providers[0].Targets = []model.TargetProjection{{Target: runtimebundle.Target{
		ID:         "target",
		Checkpoint: &runtimebundle.Checkpoint{ReadFormats: []string{"current"}},
	}}}
	err := New(repo).Reconcile(t.Context(), candidate, time.UnixMilli(1000))
	if !errors.Is(err, model.ErrProviderCheckpointUnreadable) {
		t.Fatalf("checkpoint protection lost: %v", err)
	}
}

func TestReferencedTargetCannotBeRemoved(t *testing.T) {
	repo := &reconciliationRepository{
		reconcileErr: model.ErrProviderTargetReferenced,
	}
	err := New(repo).Reconcile(
		t.Context(), businessCandidate("1.1.0", "b"), time.UnixMilli(1000),
	)
	if !errors.Is(err, model.ErrProviderTargetReferenced) {
		t.Fatalf("reference protection lost: %v", err)
	}
}

func TestActivationCallsReconcile(t *testing.T) {
	repo := &reconciliationRepository{}
	err := New(repo).Reconcile(
		t.Context(), businessCandidate("1.1.0", "b"), time.UnixMilli(1000),
	)
	if err != nil || repo.reconcileCalled != 1 {
		t.Fatalf("activation: called=%d error=%v",
			repo.reconcileCalled, err)
	}
}

func TestProjectionWriteFailurePreservesCause(t *testing.T) {
	failure := errors.New("catalog storage unavailable")
	repo := &reconciliationRepository{reconcileErr: failure}
	err := New(repo).Reconcile(
		t.Context(), businessCandidate("1.1.0", "b"), time.UnixMilli(1000),
	)
	if !errors.Is(err, failure) {
		t.Fatalf("publication failure: error=%v", err)
	}
}

func businessCandidate(version, digest string) model.Projection {
	return model.Projection{
		CatalogSHA256: strings.Repeat("c", 64),
		Providers: []model.ProviderProjection{{
			Active: runtimebundle.ActiveProvider{
				ProviderID:      "provider",
				ProviderVersion: version,
				BundleSHA256:    strings.Repeat(digest, 64),
			},
		}},
	}
}

type reconciliationRepository struct {
	reconcileCalled int
	reconcileErr    error
}

func (repo *reconciliationRepository) CommitReconcile(
	_ context.Context, _ model.ReconcileCommand,
) error {
	repo.reconcileCalled++
	return repo.reconcileErr
}
