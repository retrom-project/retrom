package runtimeprovider

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"retrom/internal/runtimebundle"
)

func TestRejectedVersionDoesNotWriteProjection(t *testing.T) {
	for _, test := range []struct {
		version, digest string
		expected        error
	}{
		{"0.9.0", "b", ErrProviderDowngrade}, {"1.0.0", "b", ErrProviderVersionRebuilt},
	} {
		repo := &reconciliationRepository{current: CurrentState{Providers: map[string]CurrentProvider{
			"provider": {Version: "1.0.0", BundleSHA256: strings.Repeat("a", 64)},
		}}}
		err := New(repo).Reconcile(t.Context(), businessCandidate(test.version, test.digest), time.UnixMilli(1000))
		if !errors.Is(err, test.expected) || len(repo.writes) != 0 {
			t.Fatalf("rejected upgrade wrote state: %v %v", repo.writes, err)
		}
	}
}

func TestUnchangedProjectionDoesNotWrite(t *testing.T) {
	candidate := businessCandidate("1.0.0", "a")
	repo := &reconciliationRepository{current: CurrentState{Providers: map[string]CurrentProvider{
		"provider": {Version: "1.0.0", BundleSHA256: strings.Repeat("a", 64)},
	}, CatalogSHA256: candidate.CatalogSHA256}}
	if err := New(repo).Reconcile(t.Context(), candidate, time.UnixMilli(1000)); err != nil {
		t.Fatal(err)
	}
	if len(repo.writes) != 0 {
		t.Fatalf("unchanged activation wrote state: %v", repo.writes)
	}
}

func TestUnreadableCheckpointRejectsBeforePublication(t *testing.T) {
	repo := &reconciliationRepository{formats: []string{"legacy"}}
	candidate := businessCandidate("1.1.0", "b")
	candidate.Providers[0].Targets = []TargetProjection{{Target: runtimebundle.Target{
		ID:         "target",
		Checkpoint: &runtimebundle.Checkpoint{ReadFormats: []string{"current"}},
	}}}
	err := New(repo).Reconcile(t.Context(), candidate, time.UnixMilli(1000))
	if !errors.Is(err, ErrProviderCheckpointUnreadable) || len(repo.writes) != 0 {
		t.Fatalf("checkpoint protection lost: %v %v", repo.writes, err)
	}
}

func TestReferencedTargetCannotBeRemoved(t *testing.T) {
	repo := &reconciliationRepository{referenced: true, current: CurrentState{Targets: []TargetIdentity{{ProviderID: "old", TargetID: "target"}}}}
	err := New(repo).Reconcile(t.Context(), businessCandidate("1.1.0", "b"), time.UnixMilli(1000))
	if !errors.Is(err, ErrProviderTargetReferenced) || len(repo.writes) != 0 {
		t.Fatalf("reference protection lost: %v %v", repo.writes, err)
	}
}

func TestActivationUsesOneTransactionAndOrderedWrites(t *testing.T) {
	repo := &reconciliationRepository{}
	err := New(repo).Reconcile(t.Context(), businessCandidate("1.1.0", "b"), time.UnixMilli(1000))
	if err != nil || repo.transactions != 1 || !reflect.DeepEqual(repo.writes, []string{"publish", "audit"}) {
		t.Fatalf("activation workflow: transactions=%d writes=%v error=%v", repo.transactions, repo.writes, err)
	}
}

func TestProjectionWriteFailurePreservesCause(t *testing.T) {
	failure := errors.New("catalog storage unavailable")
	repo := &reconciliationRepository{publishError: failure}
	err := New(repo).Reconcile(t.Context(), businessCandidate("1.1.0", "b"), time.UnixMilli(1000))
	if !errors.Is(err, failure) || !reflect.DeepEqual(repo.writes, []string{"publish"}) {
		t.Fatalf("publication failure: writes=%v error=%v", repo.writes, err)
	}
}

func businessCandidate(version, digest string) Projection {
	return Projection{CatalogSHA256: strings.Repeat("c", 64), Providers: []ProviderProjection{{Active: runtimebundle.ActiveProvider{
		ProviderID: "provider", ProviderVersion: version, BundleSHA256: strings.Repeat(digest, 64),
	}}}}
}

type reconciliationRepository struct {
	current      CurrentState
	formats      []string
	referenced   bool
	transactions int
	writes       []string
	publishError error
}

func (repo *reconciliationRepository) WithWrite(_ context.Context, work func(WriteScope) error) error {
	repo.transactions++
	return work(WriteScope{Catalog: repo, Projection: repo})
}

func (repo *reconciliationRepository) Current(context.Context) (CurrentState, error) {
	return repo.current, nil
}

func (repo *reconciliationRepository) CheckpointFormats(context.Context, TargetIdentity) ([]string, error) {
	return repo.formats, nil
}

func (repo *reconciliationRepository) TargetReferenced(context.Context, TargetIdentity) (bool, error) {
	return repo.referenced, nil
}

func (repo *reconciliationRepository) Publish(context.Context, Publication) error {
	repo.writes = append(repo.writes, "publish")
	return repo.publishError
}

func (repo *reconciliationRepository) Audit(context.Context, Audit) error {
	repo.writes = append(repo.writes, "audit")
	return nil
}
