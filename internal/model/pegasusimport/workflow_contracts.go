package pegasusimport

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

type WorkflowScope struct {
	Payload payload.ReleaseScope
	Read    WorkflowReader
	Write   WorkflowWriter
}

type WorkflowReader interface {
	Current(context.Context, string) (WorkflowSnapshot, error)
	CurrentJob(context.Context, string) (WorkflowSnapshot, error)
}

type WorkflowWriter interface {
	Cancel(context.Context, CancellationPlan) error
	Retry(context.Context, RetryPlan) error
}

type WorkflowRepository interface {
	WithControl(context.Context, func(WorkflowScope) error) error
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
