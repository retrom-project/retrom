package launch

import (
	"context"
	"time"
)

type ValidationRunner interface {
	Run(context.Context, string) error
	Recover(context.Context) ([]string, error)
}

// ValidationSupervisor owns every validation goroutine and its cancellation lifetime.
type ValidationSupervisor struct {
	worker ValidationRunner
	runs   *validationWorkerRuns
	report func(error)
}

func NewValidationSupervisor(worker ValidationRunner, report func(error)) *ValidationSupervisor {
	return &ValidationSupervisor{worker: worker, runs: newValidationWorkerRuns(), report: report}
}

func (supervisor *ValidationSupervisor) Resume(parent context.Context, id string) {
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(context.Canceled)
	done, accepted := supervisor.runs.register(cancel)
	if !accepted {
		return
	}
	defer done()
	supervisor.failed(supervisor.worker.Run(ctx, id))
}

// Dispatch registers the child before returning so Close can cancel and join it.
func (supervisor *ValidationSupervisor) Dispatch(parent context.Context, id string) {
	ctx, cancel := context.WithCancelCause(parent)
	done, accepted := supervisor.runs.register(cancel)
	if !accepted {
		return
	}
	go func() {
		defer done()
		defer cancel(context.Canceled)
		supervisor.failed(supervisor.worker.Run(ctx, id))
	}()
}

func (supervisor *ValidationSupervisor) Recover() {
	ctx, timeout := context.WithTimeout(context.Background(), 10*time.Second)
	defer timeout()
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(context.Canceled)
	done, accepted := supervisor.runs.register(cancel)
	if !accepted {
		return
	}
	defer done()
	ids, err := supervisor.worker.Recover(ctx)
	if err != nil {
		supervisor.failed(err)
		return
	}
	for _, id := range ids {
		supervisor.Dispatch(context.Background(), id)
	}
}

func (supervisor *ValidationSupervisor) failed(err error) {
	if err != nil && supervisor.report != nil {
		supervisor.report(err)
	}
}

func (supervisor *ValidationSupervisor) Close() { supervisor.runs.Close() }
