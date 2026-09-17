package pegasusimport

import (
	"context"
	"errors"
	"fmt"
	model "retrom/internal/model/pegasusimport"
	"sync"
	"time"
)

type WorkerLeases interface {
	Claim(context.Context) (model.Work, bool, error)
	Renew(context.Context, model.ExecutionIdentity) error
}
type (
	WorkerMaintenance interface{ Maintain(context.Context) error }
	WorkerExecutor    interface {
		Execute(context.Context, model.Work)
	}
	WorkerCancellation interface {
		Cancelled(context.Context, model.ExecutionIdentity) (bool, error)
		CloseCancelled(context.Context, model.ExecutionIdentity) (bool, error)
	}
)

type WorkerDependencies struct {
	Leases       WorkerLeases
	Maintenance  WorkerMaintenance
	Executor     WorkerExecutor
	Cancellation WorkerCancellation
	Report       func(error)
}

// Worker owns the lifetimes of the queue, independent maintenance, and execution monitors.
// Its collaborators expose domain operations and never pass SQL handles across this boundary.
type Worker struct {
	dependencies                                 WorkerDependencies
	mutex                                        sync.Mutex
	started, closed                              bool
	cancel                                       context.CancelFunc
	wait                                         sync.WaitGroup
	queueWake, maintenanceWake, cancellationWake chan struct{}
}

func NewWorker(dependencies WorkerDependencies) *Worker {
	return &Worker{
		dependencies: dependencies, queueWake: make(chan struct{}, 1),
		maintenanceWake: make(chan struct{}, 1), cancellationWake: make(chan struct{}, 1),
	}
}

func (worker *Worker) Start() {
	worker.mutex.Lock()
	defer worker.mutex.Unlock()
	if worker.started || worker.closed {
		return
	}
	worker.started = true
	ctx, cancel := context.WithCancel(context.Background())
	worker.cancel = cancel
	worker.wait.Add(2)
	go func() { defer worker.wait.Done(); worker.runQueue(ctx) }()
	go func() { defer worker.wait.Done(); worker.runMaintenance(ctx) }()
}

func (worker *Worker) Close() {
	worker.mutex.Lock()
	worker.closed = true
	if worker.cancel != nil {
		worker.cancel()
	}
	worker.mutex.Unlock()
	worker.wait.Wait()
}

func (worker *Worker) Signal() {
	for _, wake := range []chan struct{}{worker.queueWake, worker.maintenanceWake, worker.cancellationWake} {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}

func (worker *Worker) runQueue(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		worker.drainQueue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-worker.queueWake:
		}
	}
}

func (worker *Worker) drainQueue(ctx context.Context) {
	for ctx.Err() == nil {
		unit, found, err := worker.dependencies.Leases.Claim(ctx)
		if err != nil {
			worker.report(ctx, fmt.Errorf("claim Pegasus worker: %w", err))
			return
		}
		if !found || ctx.Err() != nil {
			return
		}
		worker.Run(ctx, unit)
	}
}

func (worker *Worker) runMaintenance(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := worker.dependencies.Maintenance.Maintain(bounded)
		cancel()
		if err != nil {
			worker.report(ctx, fmt.Errorf("maintain Pegasus worker: %w", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-worker.maintenanceWake:
		}
	}
}

func (worker *Worker) report(ctx context.Context, err error) {
	if err == nil || ctx.Err() != nil || errors.Is(err, model.ErrVersionConflict) {
		return
	}
	if worker.dependencies.Report != nil {
		worker.dependencies.Report(err)
	}
}
