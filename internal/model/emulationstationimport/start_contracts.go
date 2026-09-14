package emulationstationimport

import (
	"context"

	payload "retrom/internal/model/payloadrelease"
)

type StartSnapshot struct {
	Summary Summary
	FrozenSourceSnapshot
	TagsValid, TargetsValid, OtherActive bool
}

type StartScope struct {
	Payload payload.ReleaseScope
	Read    StartReader
	Write   StartWriter
}

type StartReader interface {
	Current(context.Context, string) (StartSnapshot, error)
}

type StartWriter interface {
	Queue(context.Context, StartPlan) error
}

type StartRepository interface {
	Inspect(context.Context, string) (StartSnapshot, error)
	WithStart(context.Context, func(StartScope) error) error
}

type StartPlan struct {
	Before                                          StartSnapshot
	JobID, ExecutionID, AuditID, ActorID, DedupeKey string
	NowMS                                           int64
}
