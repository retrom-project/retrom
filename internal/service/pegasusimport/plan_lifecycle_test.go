package pegasusimport

import (
	"context"
	"errors"
	"testing"
	"time"
)

type planLifecycleMemory struct {
	summary        Summary
	candidates     []ExpiredPlan
	err, commitErr error
	deleted        *PlanDeletion
	expired        *PlanExpiry
}

func (m *planLifecycleMemory) WithPlanWrite(_ context.Context, work func(PlanRecords) error) error {
	if err := work(m); err != nil {
		return err
	}
	return m.commitErr
}

func (m *planLifecycleMemory) ExpiredPlans(context.Context, int64, int) ([]ExpiredPlan, error) {
	return m.candidates, m.err
}

func (m *planLifecycleMemory) Get(context.Context, string) (Summary, error) { return m.summary, m.err }

func (m *planLifecycleMemory) Delete(_ context.Context, plan PlanDeletion) error {
	m.deleted = &plan
	return m.err
}

func (m *planLifecycleMemory) Expire(_ context.Context, plan PlanExpiry) error {
	m.expired = &plan
	return m.err
}

func TestPlanDeletionUsesCurrentVersionAndActor(t *testing.T) {
	t.Parallel()
	repo := &planLifecycleMemory{summary: Summary{ID: "plan", Version: 4, State: "AWAITING_MAPPING", CreatedBy: CreatedBy{ID: "creator"}}}
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
	for _, summary := range []Summary{{ID: "plan", Version: 5, State: "AWAITING_MAPPING"}, {ID: "plan", Version: 4, State: "RUNNING"}, {ID: "plan", Version: 4, State: "EXPIRED", ImportJobID: &job}} {
		repo := &planLifecycleMemory{summary: summary}
		err := NewPlanLifecycle(repo, time.Now).Delete(t.Context(), "plan", 4, "actor")
		if !errors.Is(err, ErrInvalid) || repo.deleted != nil {
			t.Fatalf("invalid deletion: %#v, %v", summary, err)
		}
	}
}

func TestPlanExpirySkipsChangedCandidates(t *testing.T) {
	t.Parallel()
	for _, summary := range []Summary{{ID: "plan", Version: 5, State: "AWAITING_MAPPING", ExpiresAtMS: 1}, {ID: "plan", Version: 4, State: "QUEUED", ExpiresAtMS: 1}, {ID: "plan", Version: 4, State: "AWAITING_MAPPING", ExpiresAtMS: 20}} {
		repo := &planLifecycleMemory{summary: summary, candidates: []ExpiredPlan{{ID: "plan", Version: 4}}}
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
			repo := &planLifecycleMemory{summary: Summary{ID: "plan", Version: 4, State: "EXPIRED", CreatedBy: CreatedBy{ID: "creator"}}}
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
	repo := &planLifecycleMemory{summary: Summary{ID: "plan", Version: 4, State: "AWAITING_MAPPING", ExpiresAtMS: 10}, candidates: []ExpiredPlan{{ID: "plan", Version: 4}}}
	if err := NewPlanLifecycle(repo, func() time.Time { return time.UnixMilli(10) }).Expire(t.Context()); err != nil {
		t.Fatal(err)
	}
	if repo.expired == nil || repo.expired.Before.Version != 4 || repo.expired.NowMS != 10 {
		t.Fatalf("expiry: %#v", repo.expired)
	}
}
