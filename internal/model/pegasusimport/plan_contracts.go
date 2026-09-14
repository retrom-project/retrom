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

type PlanRecords interface {
	Get(context.Context, string) (Summary, error)
	Delete(context.Context, PlanDeletion) error
	Expire(context.Context, PlanExpiry) error
}

type PlanLifecycleRepository interface {
	WithPlanWrite(context.Context, func(PlanRecords) error) error
	ExpiredPlans(context.Context, int64, int) ([]ExpiredPlan, error)
}
