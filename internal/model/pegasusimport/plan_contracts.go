package pegasusimport

import "context"

type ExpiredPlan struct {
	ID      string
	Version int64
}

type PlanDeletion struct {
	Before           Summary
	ActorID, AuditID string
	NowMS            int64
}

type PlanExpiry struct {
	Before Summary
	NowMS  int64
}

type PlanLifecycleRepository interface {
	LoadPlanSummary(context.Context, string) (Summary, error)
	CommitPlanDeletion(context.Context, PlanDeletion) error
	CommitPlanExpiry(context.Context, PlanExpiry) error
	ExpiredPlans(context.Context, int64, int) ([]ExpiredPlan, error)
}
