package emulationstationimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type creationMemory struct {
	count    int
	err      error
	writeErr error
	inserted bool
	plan     model.CreationPlan
}

func (m *creationMemory) LoadPendingPlanCount(context.Context) (int, error) {
	return m.count, m.err
}

func (m *creationMemory) CommitCreation(_ context.Context, plan model.CreationPlan) (model.Summary, error) {
	m.inserted = true
	m.plan = plan
	if m.writeErr != nil {
		return model.Summary{}, m.writeErr
	}
	return model.Summary{ID: plan.ImportID}, nil
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
	if repo.plan.ReleaseYearMax != 1971 || repo.plan.NowMS != 123 || repo.plan.ExpiresAtMS != 123+(7*24*time.Hour).Milliseconds() {
		t.Fatalf("plan time: %#v", repo.plan)
	}
	assertCreationIdentities(t, repo.plan)
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
	for _, phase := range []string{"source", "count", "write"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			repo := &creationMemory{}
			source := &creationSource{}
			switch phase {
			case "source":
				source.err = cause
			case "count":
				repo.err = cause
			case "write":
				repo.writeErr = cause
			}
			value, err := NewCreation(repo, source, time.Now).Create(t.Context(), model.CreateRequest{}, "actor")
			if !errors.Is(err, cause) || value.ID != "" {
				t.Fatalf("failed creation: %#v, %v", value, err)
			}
			if phase != "write" && repo.inserted {
				t.Fatal("creation continued after failure")
			}
		})
	}
}

func assertCreationIdentities(t *testing.T, plan model.CreationPlan) {
	t.Helper()
	ids := map[string]bool{}
	for _, id := range []string{plan.ImportID, plan.JobID, plan.ExecutionID, plan.AuditID} {
		if id == "" || ids[id] {
			t.Fatalf("invalid identities: %#v", plan)
		}
		ids[id] = true
	}
}
