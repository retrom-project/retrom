package payloadrelease

import (
	"context"
	"errors"
)

var (
	ErrExecutionLost     = errors.New("PAYLOAD_RELEASE_EXECUTION_LOST")
	ErrExecutionTimeout  = errors.New("PAYLOAD_RELEASE_EXECUTION_TIMEOUT")
	ErrAttemptsExhausted = errors.New("PAYLOAD_RELEASE_ATTEMPTS_EXHAUSTED")
	ErrInputInvalid      = errors.New("PAYLOAD_RELEASE_INPUT_INVALID")
	ErrWorkerClosed      = errors.New("PAYLOAD_RELEASE_WORKER_CLOSED")
)

type WorkTime struct {
	Value int64
	Set   bool
}

type Work struct {
	ID, Kind, State, WorkerID                               string
	Scope                                                   Scope
	ExecutionNo, Attempt, MaxAttempts, Version, AvailableMS int64
	Started, Deadline, Lease, Heartbeat                     WorkTime
	InputJSON, InputDigest                                  string
	InputFound                                              bool
}

type Execution struct {
	Work  Work
	Input Input
}

type WorkChange struct {
	OwnerFailure                    *Owner
	Before, After                   Work
	NowMS                           int64
	EventType, EventJSON, ErrorCode string
	Retryable                       bool
	AuditID, AuditAction, AuditJSON string
}

type FailureOwnerReader interface {
	Owner(context.Context, Scope) (Owner, error)
}

type WorkerReader interface {
	Next(context.Context, int64) (Work, bool, error)
	Current(context.Context, string) (Work, bool, error)
	Interrupted(context.Context, int64, int) ([]Work, error)
}

type WorkerWriter interface {
	Change(context.Context, WorkChange) error
	Fence(context.Context, Work) error
}

type WorkerScope struct {
	Read   WorkerReader
	Write  WorkerWriter
	Owners FailureOwnerReader
}

type WorkerRepository interface {
	WithWorker(context.Context, func(WorkerScope) error) error
}

type WorkExecutor interface {
	Execute(context.Context, Execution) error
}
