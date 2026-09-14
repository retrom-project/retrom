package emulationstationimport

import (
	"context"

	payload "retrom/internal/model/payloadrelease"
)

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

type WorkflowScope struct {
	Payload payload.ReleaseScope
	Read    WorkflowReader
	Write   WorkflowWriter
}

type WorkflowReader interface {
	Current(context.Context, string) (WorkflowSnapshot, error)
	RetryCurrent(context.Context, string) (RetrySnapshot, error)
}

type WorkflowWriter interface {
	Cancel(context.Context, CancellationPlan) error
	Retry(context.Context, RetryPlan) error
}

type WorkflowRepository interface {
	InspectRetry(context.Context, string) (RetrySnapshot, error)
	WithControl(context.Context, func(WorkflowScope) error) error
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
