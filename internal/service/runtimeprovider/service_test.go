package runtimeprovider

import (
	"context"
	"errors"
	"reflect"
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
		{"0.9.0", "b", model.ErrProviderDowngrade}, {"1.0.0", "b", model.ErrProviderVersionRebuilt},
	} {
		repo := &reconciliationRepository{current: model.CurrentState{Providers: map[string]model.CurrentProvider{
			"provider": {Version: "1.0.0", BundleSHA256: strings.Repeat("a", 64)},
		}}}
		err := New(repo).Reconcile(t.Context(), businessCandidate(test.version, test.digest), time.UnixMilli(1000))
		if !errors.Is(err, test.expected) || len(repo.writes) != 0 {
			t.Fatalf("rejected upgrade wrote state: %v %v", repo.writes, err)
		}
	}
}

func TestUnchangedProjectionDoesNotInterruptSessions(t *testing.T) {
	candidate := businessCandidate("1.0.0", "a")
	repo := &reconciliationRepository{current: model.CurrentState{Providers: map[string]model.CurrentProvider{
		"provider": {Version: "1.0.0", BundleSHA256: strings.Repeat("a", 64)},
	}, CatalogSHA256: candidate.CatalogSHA256}}
	if err := New(repo).Reconcile(t.Context(), candidate, time.UnixMilli(1000)); err != nil {
		t.Fatal(err)
	}
	if len(repo.writes) != 0 {
		t.Fatalf("unchanged activation wrote state: %v", repo.writes)
	}
}

func TestUnreadableCheckpointRejectsBeforeTermination(t *testing.T) {
	repo := &reconciliationRepository{formats: []string{"legacy"}}
	candidate := businessCandidate("1.1.0", "b")
	candidate.Providers[0].Targets = []model.TargetProjection{{Target: runtimebundle.Target{
		ID:         "target",
		Checkpoint: &runtimebundle.Checkpoint{ReadFormats: []string{"current"}},
	}}}
	err := New(repo).Reconcile(t.Context(), candidate, time.UnixMilli(1000))
	if !errors.Is(err, model.ErrProviderCheckpointUnreadable) || len(repo.writes) != 0 {
		t.Fatalf("checkpoint protection lost: %v %v", repo.writes, err)
	}
}

func TestReferencedTargetCannotBeRemoved(t *testing.T) {
	repo := &reconciliationRepository{referenced: true, current: model.CurrentState{Targets: []model.TargetIdentity{{ProviderID: "old", TargetID: "target"}}}}
	err := New(repo).Reconcile(t.Context(), businessCandidate("1.1.0", "b"), time.UnixMilli(1000))
	if !errors.Is(err, model.ErrProviderTargetReferenced) || len(repo.writes) != 0 {
		t.Fatalf("reference protection lost: %v %v", repo.writes, err)
	}
}

func TestActivationUsesOneTransactionAndOrderedWrites(t *testing.T) {
	repo := &reconciliationRepository{}
	err := New(repo).Reconcile(t.Context(), businessCandidate("1.1.0", "b"), time.UnixMilli(1000))
	if err != nil || repo.transactions != 1 || !reflect.DeepEqual(repo.writes, []string{"terminate:provider", "publish", "audit"}) {
		t.Fatalf("activation workflow: transactions=%d writes=%v error=%v", repo.transactions, repo.writes, err)
	}
}

func TestProjectionWriteFailurePreservesCause(t *testing.T) {
	failure := errors.New("catalog storage unavailable")
	repo := &reconciliationRepository{publishError: failure}
	err := New(repo).Reconcile(t.Context(), businessCandidate("1.1.0", "b"), time.UnixMilli(1000))
	if !errors.Is(err, failure) || !reflect.DeepEqual(repo.writes, []string{"terminate:provider", "publish"}) {
		t.Fatalf("publication failure: writes=%v error=%v", repo.writes, err)
	}
}

func businessCandidate(version, digest string) model.Projection {
	return model.Projection{CatalogSHA256: strings.Repeat("c", 64), Providers: []model.ProviderProjection{{Active: runtimebundle.ActiveProvider{
		ProviderID: "provider", ProviderVersion: version, BundleSHA256: strings.Repeat(digest, 64),
	}}}}
}

type reconciliationRepository struct {
	current      model.CurrentState
	formats      []string
	referenced   bool
	transactions int
	writes       []string
	publishError error
}

func (repo *reconciliationRepository) WithWrite(_ context.Context, work func(model.WriteScope) error) error {
	repo.transactions++
	return work(model.WriteScope{Catalog: repo, Projection: repo})
}

func (repo *reconciliationRepository) Current(context.Context) (model.CurrentState, error) {
	return repo.current, nil
}

func (repo *reconciliationRepository) CheckpointFormats(context.Context, model.TargetIdentity) ([]string, error) {
	return repo.formats, nil
}

func (repo *reconciliationRepository) TargetReferenced(context.Context, model.TargetIdentity) (bool, error) {
	return repo.referenced, nil
}

func (repo *reconciliationRepository) Publish(context.Context, model.Publication) error {
	repo.writes = append(repo.writes, "publish")
	return repo.publishError
}

func (repo *reconciliationRepository) TerminateSessions(_ context.Context, id string, _ int64) error {
	repo.writes = append(repo.writes, "terminate:"+id)
	return nil
}

func (repo *reconciliationRepository) Audit(context.Context, model.Audit) error {
	repo.writes = append(repo.writes, "audit")
	return nil
}
