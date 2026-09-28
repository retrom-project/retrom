package application

import (
	"context"
	"errors"
	"log/slog"
)

// Start and Stop are serialized by application startup/shutdown. Wait joins the entire indexing operation.
type catalogTask struct {
	run    func(context.Context) error
	cancel context.CancelFunc
	done   chan struct{}
}

func newCatalogTask(run func(context.Context) error) *catalogTask {
	return &catalogTask{run: run}
}

func (task *catalogTask) Start(parent context.Context) {
	if task.run == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	task.cancel, task.done = cancel, make(chan struct{})
	go func() {
		defer close(task.done)
		if err := task.run(ctx); err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.Error("background DAT indexing failed", "error", err)
			}
			return
		}
		slog.Info("background DAT indexing complete")
	}()
}

func (task *catalogTask) Stop() {
	if task.cancel != nil {
		task.cancel()
	}
}

func (task *catalogTask) Wait() {
	if task.done != nil {
		<-task.done
	}
}
