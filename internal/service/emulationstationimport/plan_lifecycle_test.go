package emulationstationimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type planLifecycleMemory struct {
	summary             model.Summary
	candidates          []model.ExpiredPlan
	err, commitErr      error
	lookupErr, writeErr error
	candidateLimit      int
	candidateTime       int64
	deleted             *model.PlanDeletion
	expired             *model.PlanExpiry
}

func (m *planLifecycleMemory) WithPlanWrite(_ context.Context, work func(model.PlanRecords) error) error {
	if err := work(m); err != nil {
		return err
	}
	return m.commitErr
}

func (m *planLifecycleMemory) ExpiredPlans(_ context.Context, now int64, limit int) ([]model.ExpiredPlan, error) {
	m.candidateLimit, m.candidateTime = limit, now
	return m.candidates, m.err
}

func (m *planLifecycleMemory) Get(context.Context, string) (model.Summary, error) {
	if m.lookupErr != nil {
		return model.Summary{}, m.lookupErr
	}
	return m.summary, m.err
}

func (m *planLifecycleMemory) Delete(_ context.Context, plan model.PlanDeletion) error {
	m.deleted = &plan
	return m.writeErr
}

func (m *planLifecycleMemory) Expire(_ context.Context, plan model.PlanExpiry) error {
	m.expired = &plan
	return m.writeErr
}

func TestPlanDeletionUsesCurrentVersionAndActor(t *testing.T) {
	t.Parallel()
	repo := &planLifecycleMemory{summary: model.Summary{ID: "plan", Version: 4, State: "AWAITING_MAPPING", CreatedBy: model.CreatedBy{ID: "creator"}}}
	if err := NewPlanLifecycle(repo, func() time.Time { return time.UnixMilli(10) }).Delete(t.Context(), "plan", 4, "admin"); err != nil {
		t.Fatal(err)
	}
	if repo.deleted == nil || repo.deleted.ActorID != "admin" || repo.deleted.Before.Version != 4 || repo.deleted.NowMS != 10 || repo.deleted.AuditID == "" {
		t.Fatalf("deletion: %#v", repo.deleted)
	}
}

func TestPlanDeletionRejectsStaleOrStartedPlans(t *testing.T) {
	t.Parallel()
	job := "job"
	for _, summary := range []model.Summary{{ID: "plan", Version: 5, State: "AWAITING_MAPPING"}, {ID: "plan", Version: 4, State: "RUNNING"}, {ID: "plan", Version: 4, State: "EXPIRED", ImportJobID: &job}} {
		repo := &planLifecycleMemory{summary: summary}
		err := NewPlanLifecycle(repo, time.Now).Delete(t.Context(), "plan", 4, "actor")
		if !errors.Is(err, model.ErrInvalid) || repo.deleted != nil {
			t.Fatalf("invalid deletion: %#v, %v", summary, err)
		}
	}
}

func TestPlanExpirySkipsChangedCandidates(t *testing.T) {
	t.Parallel()
	for _, summary := range []model.Summary{{ID: "plan", Version: 5, State: "AWAITING_MAPPING", ExpiresAtMS: 1}, {ID: "plan", Version: 4, State: "QUEUED", ExpiresAtMS: 1}, {ID: "plan", Version: 4, State: "AWAITING_MAPPING", ExpiresAtMS: 20}} {
		repo := &planLifecycleMemory{summary: summary, candidates: []model.ExpiredPlan{{ID: "plan", Version: 4}}}
		if err := NewPlanLifecycle(repo, func() time.Time { return time.UnixMilli(10) }).Expire(t.Context()); err != nil {
			t.Fatal(err)
		}
		if repo.expired != nil {
			t.Fatalf("expired a changed candidate: %#v", summary)
		}
	}
}

func TestPlanLifecyclePropagatesReadAndCommitFailure(t *testing.T) {
	t.Parallel()
	cause := errors.New("storage failure")
	for _, phase := range []string{"read", "commit"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			repo := &planLifecycleMemory{summary: model.Summary{ID: "plan", Version: 4, State: "EXPIRED", CreatedBy: model.CreatedBy{ID: "creator"}}}
			if phase == "read" {
				repo.err = cause
			} else {
				repo.commitErr = cause
			}
			if err := NewPlanLifecycle(repo, time.Now).Delete(t.Context(), "plan", 4, ""); !errors.Is(err, cause) {
				t.Fatalf("lost cause: %v", err)
			}
		})
	}
}

func TestPlanExpiryWritesOnlyCurrentExpiredMappingPlan(t *testing.T) {
	t.Parallel()
	repo := &planLifecycleMemory{summary: model.Summary{ID: "plan", Version: 4, State: "AWAITING_MAPPING", ExpiresAtMS: 10}, candidates: []model.ExpiredPlan{{ID: "plan", Version: 4}}}
	if err := NewPlanLifecycle(repo, func() time.Time { return time.UnixMilli(10) }).Expire(t.Context()); err != nil {
		t.Fatal(err)
	}
	if repo.expired == nil || repo.expired.Before.Version != 4 || repo.expired.NowMS != 10 {
		t.Fatalf("expiry: %#v", repo.expired)
	}
}

func TestPlanDeletionFallsBackToCreator(t *testing.T) {
	t.Parallel()
	repo := &planLifecycleMemory{summary: model.Summary{ID: "plan", Version: 1, State: "EXPIRED", CreatedBy: model.CreatedBy{ID: "creator"}}}
	if err := NewPlanLifecycle(repo, time.Now).Delete(t.Context(), "plan", 1, ""); err != nil {
		t.Fatal(err)
	}
	if repo.deleted == nil || repo.deleted.ActorID != "creator" {
		t.Fatalf("deletion actor=%#v", repo.deleted)
	}
}

func TestPlanExpiryBoundsCandidateReadAndSkipsMissingPlans(t *testing.T) {
	t.Parallel()
	repo := &planLifecycleMemory{candidates: []model.ExpiredPlan{{ID: "missing", Version: 1}}, lookupErr: model.ErrNotFound}
	if err := NewPlanLifecycle(repo, func() time.Time { return time.UnixMilli(10) }).Expire(t.Context()); err != nil {
		t.Fatal(err)
	}
	if repo.expired != nil || repo.candidateLimit != 100 || repo.candidateTime != 10 {
		t.Fatalf("expiry read=%#v", repo)
	}
}

func TestPlanExpiryPreservesFailuresFromEveryStage(t *testing.T) {
	t.Parallel()
	cause := errors.New("expiry failed")
	for _, phase := range []string{"candidates", "read", "write", "commit"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			repo := &planLifecycleMemory{summary: model.Summary{ID: "plan", Version: 1, State: "AWAITING_MAPPING", ExpiresAtMS: 10}, candidates: []model.ExpiredPlan{{ID: "plan", Version: 1}}}
			switch phase {
			case "candidates":
				repo.err = cause
			case "read":
				repo.lookupErr = cause
			case "write":
				repo.writeErr = cause
			case "commit":
				repo.commitErr = cause
			}
			err := NewPlanLifecycle(repo, func() time.Time { return time.UnixMilli(10) }).Expire(t.Context())
			if !errors.Is(err, cause) {
				t.Fatalf("lost %s cause: %v", phase, err)
			}
		})
	}
}
