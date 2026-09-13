package launch

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

type supervisorWorker struct {
	calls   atomic.Int32
	started chan struct{}
	ended   chan error
}

func (worker *supervisorWorker) Run(ctx context.Context, _ string) error {
	worker.calls.Add(1)
	close(worker.started)
	<-ctx.Done()
	worker.ended <- context.Cause(ctx)
	return context.Cause(ctx)
}
func (*supervisorWorker) Recover(context.Context) ([]string, error) { return nil, nil }

func TestValidationSupervisorClosesDispatchedWorkAndRejectsNewWork(t *testing.T) {
	worker := &supervisorWorker{started: make(chan struct{}), ended: make(chan error, 1)}
	reported := make(chan error, 1)
	supervisor := NewValidationSupervisor(worker, func(err error) { reported <- err })
	supervisor.Dispatch(t.Context(), "validation")
	<-worker.started
	supervisor.Close()
	if !errors.Is(<-worker.ended, ErrValidationWorkerClosed) || !errors.Is(<-reported, ErrValidationWorkerClosed) {
		t.Fatal("supervisor did not retain close cause")
	}
	supervisor.Dispatch(t.Context(), "late")
	supervisor.Resume(t.Context(), "late-sync")
	supervisor.Recover()
	if worker.calls.Load() != 1 {
		t.Fatal("closed supervisor started new work")
	}
}
