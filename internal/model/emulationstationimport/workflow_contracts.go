package emulationstationimport

import "context"

type WorkflowSnapshot struct {
	Summary               Summary
	JobState              string
	JobVersion, Execution int64
	OtherActive           bool
	RetryableItems        int64
}

type RetrySnapshot struct {
	WorkflowSnapshot
	FrozenSourceSnapshot
	TargetsValid bool
}

type WorkflowRepository interface {
	InspectRetry(context.Context, string) (RetrySnapshot, error)
	CommitCancelWorkflow(context.Context, CancelWorkflowCommand) (WorkflowSnapshot, bool, error)
	CommitRetryWorkflow(context.Context, RetryWorkflowCommand) (Summary, error)
}

type CancelWorkflowCommand struct {
	ID, Reason, ActorID, AuditID string
	Version                      int64
	NowMS                        int64
	Job                          *CancelWorkflowJobInfo
}

type CancelWorkflowJobInfo struct {
	JobID, Kind, ScopeID string
	ExpectedVersion      int64
}

type CancellationPlan struct {
	Before                          WorkflowSnapshot
	State, Reason, ActorID, AuditID string
	Pending                         bool
	CompletedAtMS                   *int64
	NowMS                           int64
}

type RetryPlan struct {
	Before                        RetrySnapshot
	Execution                     int64
	ExecutionID, AuditID, ActorID string
	NowMS                         int64
}

type RetryWorkflowCommand struct {
	Plan    RetryPlan
	Version int64
}
