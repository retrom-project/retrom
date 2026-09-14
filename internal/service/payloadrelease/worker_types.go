package payloadrelease

import (
	"context"
	"sync"
	"time"
)

const (
	ExecutionTimeout = 30 * time.Minute
	workerLease      = time.Minute
	workerHeartbeat  = 15 * time.Second
)

type WorkerOptions struct {
	Now      func() time.Time
	NewID    func() (string, error)
	Report   func(error)
	Maintain func(context.Context) error
}

type Worker struct {
	repository      WorkerRepository
	executor        WorkExecutor
	now             func() time.Time
	newID           func() (string, error)
	report          func(error)
	maintain        func(context.Context) error
	mutex           sync.Mutex
	wait            sync.WaitGroup
	active          map[*workerRun]struct{}
	started, closed bool
	wake            chan struct{}
}
type workerRun struct{ cancel context.CancelCauseFunc }
