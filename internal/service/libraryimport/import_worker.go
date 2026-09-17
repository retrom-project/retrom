package libraryimport

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	model "retrom/internal/model/libraryimport"
)

type ImportWorker struct {
	lifetime        context.Context
	dependencies    model.ImportWorkerDependencies
	settings        model.ImportWorkerSettings
	mutex           sync.Mutex
	started, closed bool
	wait            sync.WaitGroup
	active          map[string]context.CancelCauseFunc
	wake            chan struct{}
	maintenanceWake chan struct{}
}

func NewImportWorker(dependencies model.ImportWorkerDependencies, settings model.ImportWorkerSettings) *ImportWorker {
	if settings.Now == nil {
		settings.Now = time.Now
	}
	return &ImportWorker{
		lifetime:        context.Background(),
		dependencies:    dependencies,
		settings:        settings,
		active:          map[string]context.CancelCauseFunc{},
		wake:            make(chan struct{}, 1),
		maintenanceWake: make(chan struct{}, 1),
	}
}

func (worker *ImportWorker) register(parent context.Context, key string) (context.Context, func(), error) {
	worker.mutex.Lock()
	defer worker.mutex.Unlock()
	if worker.closed {
		return nil, nil, ErrImportWorkerClosed
	}
	if worker.active[key] != nil {
		return nil, nil, model.ErrVersionConflict
	}
	ctx, cancel := context.WithCancelCause(parent)
	worker.active[key] = cancel
	worker.wait.Add(1)
	return ctx, func() {
		cancel(nil)
		worker.mutex.Lock()
		delete(worker.active, key)
		worker.mutex.Unlock()
		worker.wait.Done()
	}, nil
}

func (worker *ImportWorker) Start() { worker.start(worker.lifetime) }

func (worker *ImportWorker) start(ctx context.Context) {
	worker.mutex.Lock()
	if worker.started || worker.closed {
		worker.mutex.Unlock()
		return
	}
	worker.started = true
	worker.mutex.Unlock()
	worker.background(ctx, "import-queue", worker.runQueue)
	worker.background(ctx, "import-maintenance", worker.runMaintenance)
}

func (worker *ImportWorker) background(parent context.Context, key string, run func(context.Context)) {
	ctx, done, err := worker.register(parent, key)
	if err != nil {
		return
	}
	go func() { defer done(); run(ctx) }()
}

func (worker *ImportWorker) NotifyImportGroup(ctx context.Context, _ string) {
	worker.start(context.WithoutCancel(ctx))
	worker.signal()
}

func (worker *ImportWorker) Resume(ctx context.Context) {
	worker.start(context.WithoutCancel(ctx))
	worker.signal()
}

func (worker *ImportWorker) signal() {
	for _, ch := range []chan struct{}{worker.wake, worker.maintenanceWake} {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (worker *ImportWorker) Cancel(id string) {
	worker.mutex.Lock()
	if cancel := worker.active[id]; cancel != nil {
		cancel(context.Canceled)
	}
	worker.mutex.Unlock()
	worker.signal()
}

func (worker *ImportWorker) Close() {
	worker.mutex.Lock()
	worker.closed = true
	for _, cancel := range worker.active {
		cancel(ErrImportWorkerClosed)
	}
	worker.mutex.Unlock()
	worker.wait.Wait()
}

func (worker *ImportWorker) Recover(parent context.Context) error {
	ctx, done, err := worker.register(parent, "import-explicit-recovery")
	if err != nil {
		return err
	}
	defer done()
	if err := worker.dependencies.Recovery.Recover(ctx); err != nil {
		return fmt.Errorf("recover import queue: %w", err)
	}
	worker.start(context.WithoutCancel(ctx))
	worker.signal()
	return nil
}

func (worker *ImportWorker) report(ctx context.Context, err error) {
	if err == nil || ctx.Err() != nil || errors.Is(err, model.ErrVersionConflict) ||
		errors.Is(err, ErrImportWorkerClosed) {
		return
	}
	if worker.settings.Report != nil {
		worker.settings.Report(err)
	}
}
