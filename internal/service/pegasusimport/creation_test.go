package pegasusimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/pegasusimport"
)

type creationMemory struct {
	count          int
	err, commitErr error
	inserted       bool
	plan           model.CreationPlan
}

func (m *creationMemory) WithCreate(_ context.Context, work func(model.CreationWriter) error) error {
	if err := work(m); err != nil {
		return err
	}
	return m.commitErr
}
func (m *creationMemory) PendingPlans(context.Context) (int, error) { return m.count, m.err }
func (m *creationMemory) Insert(_ context.Context, plan model.CreationPlan) (model.Summary, error) {
	m.inserted = true
	m.plan = plan
	return model.Summary{ID: plan.ImportID}, m.err
}

type creationSource struct {
	err        error
	root, path string
}

func (s *creationSource) Select(_ context.Context, root, path string) (model.SelectedRoot, error) {
	s.root, s.path = root, path
	return model.SelectedRoot{ID: root, Label: "Games", Digest: "digest"}, s.err
}

func TestCreationFreezesSourceAndSevenDayPlan(t *testing.T) {
	t.Parallel()
	repo := &creationMemory{}
	source := &creationSource{}
	now := time.UnixMilli(123)
	value, err := NewCreation(repo, source, func() time.Time { return now }).Create(t.Context(), model.CreateRequest{RootID: "games", SourceRelativePath: "Roms"}, "actor")
	if err != nil {
		t.Fatal(err)
	}
	if !repo.inserted || value.ID == "" || value.ID != repo.plan.ImportID {
		t.Fatalf("plan: %#v, value:%#v", repo.plan, value)
	}
	if source.root != "games" || source.path != "Roms" || repo.plan.Root.Label != "Games" || repo.plan.Root.Digest != "digest" || repo.plan.ActorID != "actor" {
		t.Fatalf("source or actor changed: %#v", repo.plan)
	}
	if repo.plan.NowMS != 123 || repo.plan.ExpiresAtMS != 123+(7*24*time.Hour).Milliseconds() {
		t.Fatalf("plan time: %#v", repo.plan)
	}
	ids := map[string]bool{}
	for _, id := range []string{repo.plan.ImportID, repo.plan.JobID, repo.plan.ExecutionID, repo.plan.AuditID} {
		if id == "" || ids[id] {
			t.Fatalf("invalid identities: %#v", repo.plan)
		}
		ids[id] = true
	}
}

func TestCreationRejectsCapacityWithinWriteScope(t *testing.T) {
	t.Parallel()
	repo := &creationMemory{count: 20}
	value, err := NewCreation(repo, &creationSource{}, time.Now).Create(t.Context(), model.CreateRequest{}, "actor")
	if !errors.Is(err, model.ErrActive) || value.ID != "" || repo.inserted {
		t.Fatalf("capacity: %#v, %v, inserted=%v", value, err, repo.inserted)
	}
}

func TestCreationFailureReturnsNoPartialPlan(t *testing.T) {
	t.Parallel()
	cause := errors.New("failed")
	for _, phase := range []string{"source", "count", "commit"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			repo := &creationMemory{}
			source := &creationSource{}
			switch phase {
			case "source":
				source.err = cause
			case "count":
				repo.err = cause
			case "commit":
				repo.commitErr = cause
			}
			value, err := NewCreation(repo, source, time.Now).Create(t.Context(), model.CreateRequest{}, "actor")
			if !errors.Is(err, cause) || value.ID != "" {
				t.Fatalf("failed creation: %#v, %v", value, err)
			}
			if phase != "commit" && repo.inserted {
				t.Fatal("creation continued after failure")
			}
		})
	}
}
