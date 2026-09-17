package pegasusimport

import "context"

type WorkflowSnapshot struct {
	Summary               Summary
	JobState              string
	JobVersion, Execution int64
	OtherActive           bool
	RetryableItems        int64
}

type WorkflowRepository interface {
	CommitCancelWorkflow(context.Context, CancelWorkflowCommand) (WorkflowSnapshot, bool, error)
	CommitRetryWorkflow(context.Context, RetryWorkflowCommand) (Summary, error)
}

type CancelWorkflowCommand struct {
	ID, Reason, ActorID, AuditID string
	Version                       int64
	NowMS                         int64
	ByJob                         bool
	Kind, ScopeID                 string
}

type RetryWorkflowCommand struct {
	ID          string
	ActorID     string
	Version     int64
	NowMS       int64
	ExecutionID string
	AuditID     string
}

type CancellationPlan struct {
	Before                   WorkflowSnapshot
	State                    string
	Pending                  bool
	CompletedAtMS            *int64
	Reason, ActorID, AuditID string
	NowMS                    int64
}

type RetryPlan struct {
	Before                        WorkflowSnapshot
	Execution                     int64
	ExecutionID, AuditID, ActorID string
	NowMS                         int64
}
