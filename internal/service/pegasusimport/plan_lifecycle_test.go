package pegasusimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/pegasusimport"
)

type planLifecycleMemory struct {
	summary    model.Summary
	candidates []model.ExpiredPlan
	err        error
	deleted    *model.PlanDeletion
	expired    *model.PlanExpiry
	commitErr  error
}

func (m *planLifecycleMemory) LoadPlanSummary(_ context.Context, _ string) (model.Summary, error) {
	if m.err != nil {
		return model.Summary{}, m.err
	}
	return m.summary, nil
}

func (m *planLifecycleMemory) CommitPlanDeletion(_ context.Context, plan model.PlanDeletion) error {
	m.deleted = &plan
	if m.commitErr != nil {
		return m.commitErr
	}
	return nil
}

func (m *planLifecycleMemory) CommitPlanExpiry(_ context.Context, plan model.PlanExpiry) error {
	m.expired = &plan
	if m.commitErr != nil {
		return m.commitErr
	}
	return nil
}

func (m *planLifecycleMemory) ExpiredPlans(context.Context, int64, int) ([]model.ExpiredPlan, error) {
	return m.candidates, m.err
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
