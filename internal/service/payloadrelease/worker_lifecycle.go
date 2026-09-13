package payloadrelease

import (
	"context"
	"time"
)

func (worker *Worker) register(parent context.Context) (context.Context, func(), bool) {
	ctx, cancel := context.WithCancelCause(parent)
	worker.mutex.Lock()
	defer worker.mutex.Unlock()
	if worker.closed {
		cancel(ErrWorkerClosed)
		return ctx, func() {}, false
	}
	run := &workerRun{cancel: cancel}
	worker.active[run] = struct{}{}
	worker.wait.Add(1)
	return ctx, func() {
		cancel(context.Canceled)
		worker.mutex.Lock()
		delete(worker.active, run)
		worker.mutex.Unlock()
		worker.wait.Done()
	}, true
}

func (worker *Worker) Start() {
	worker.mutex.Lock()
	if worker.started || worker.closed {
		worker.mutex.Unlock()
		return
	}
	worker.started = true
	worker.mutex.Unlock()
	worker.background(worker.loop)
	worker.background(worker.maintenance)
	worker.Signal()
}

func (worker *Worker) background(run func(context.Context)) {
	ctx, done, accepted := worker.register(context.Background())
	if !accepted {
		return
	}
	go func() { defer done(); run(ctx) }()
}

func (worker *Worker) Close() {
	worker.mutex.Lock()
	worker.closed = true
	pending := make([]*workerRun, 0, len(worker.active))
	for run := range worker.active {
		pending = append(pending, run)
	}
	worker.mutex.Unlock()
	for _, run := range pending {
		run.cancel(ErrWorkerClosed)
	}
	worker.wait.Wait()
}

func (worker *Worker) Signal() {
	select {
	case worker.wake <- struct{}{}:
	default:
	}
}

func (worker *Worker) failed(err error) {
	if err != nil && worker.report != nil {
		worker.report(err)
	}
}

func (worker *Worker) loop(ctx context.Context) {
	poll := time.NewTicker(250 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-worker.wake:
		case <-poll.C:
		}
		for ctx.Err() == nil {
			did, err := worker.RunOnce(ctx)
			if err != nil {
				worker.failed(err)
				break
			}
			if !did {
				break
			}
		}
	}
}

func (worker *Worker) maintenance(ctx context.Context) {
	worker.failed(worker.Recover(ctx))
	worker.reconcile(ctx)
	recoverTick := time.NewTicker(time.Second)
	maintainTick := time.NewTicker(time.Hour)
	defer recoverTick.Stop()
	defer maintainTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-recoverTick.C:
			worker.failed(worker.Recover(ctx))
		case <-maintainTick.C:
			worker.reconcile(ctx)
		}
	}
}

func (worker *Worker) reconcile(ctx context.Context) {
	if worker.maintain != nil {
		worker.failed(worker.maintain(ctx))
	}
}
