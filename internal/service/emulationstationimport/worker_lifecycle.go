package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	model "retrom/internal/model/emulationstationimport"
	"sync"
	"time"
)

type WorkerLeases interface {
	Claim(context.Context) (model.Execution, bool, error)
	Renew(context.Context, model.Execution) (model.LeaseState, error)
}
type (
	WorkerMaintenance interface{ Maintain(context.Context) error }
	WorkerExecutor    interface {
		Execute(context.Context, model.Execution)
	}
)

type WorkerControl interface {
	Observe(context.Context, model.Execution) (model.LeaseState, error)
	CloseCancelled(context.Context, model.Execution) (bool, error)
	Fail(context.Context, model.Execution, model.ExecutionFailure) (string, error)
}
type WorkerDependencies struct {
	Leases      WorkerLeases
	Maintenance WorkerMaintenance
	Executor    WorkerExecutor
	Control     WorkerControl
	Report      func(error)
}
type Worker struct {
	dependencies                                 WorkerDependencies
	now                                          func() time.Time
	mutex                                        sync.Mutex
	started, closed                              bool
	cancel                                       context.CancelFunc
	wait                                         sync.WaitGroup
	queueWake, maintenanceWake, cancellationWake chan struct{}
}

func NewWorker(dependencies WorkerDependencies, now func() time.Time) *Worker {
	return &Worker{
		dependencies: dependencies, now: now, queueWake: make(chan struct{}, 1),
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
			worker.report(ctx, fmt.Errorf("claim EmulationStation worker: %w", err))
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
			worker.report(ctx, fmt.Errorf("maintain EmulationStation worker: %w", err))
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
