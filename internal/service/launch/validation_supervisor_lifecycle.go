package launch

import (
	"context"
	"errors"
	"sync"
)

var ErrValidationWorkerClosed = errors.New("validation worker closed")

type (
	validationWorkerRun  struct{ cancel context.CancelCauseFunc }
	validationWorkerRuns struct {
		mutex  sync.Mutex
		wait   sync.WaitGroup
		closed bool
		active map[*validationWorkerRun]struct{}
	}
)

func newValidationWorkerRuns() *validationWorkerRuns {
	return &validationWorkerRuns{active: make(map[*validationWorkerRun]struct{})}
}

func (runs *validationWorkerRuns) register(cancel context.CancelCauseFunc) (func(), bool) {
	runs.mutex.Lock()
	defer runs.mutex.Unlock()
	if runs.closed {
		cancel(ErrValidationWorkerClosed)
		return func() {}, false
	}
	run := &validationWorkerRun{cancel: cancel}
	runs.active[run] = struct{}{}
	runs.wait.Add(1)
	return func() {
		runs.mutex.Lock()
		delete(runs.active, run)
		runs.mutex.Unlock()
		runs.wait.Done()
	}, true
}

func (runs *validationWorkerRuns) Close() {
	runs.mutex.Lock()
	runs.closed = true
	pending := make([]*validationWorkerRun, 0, len(runs.active))
	for run := range runs.active {
		pending = append(pending, run)
	}
	runs.mutex.Unlock()
	for _, run := range pending {
		run.cancel(ErrValidationWorkerClosed)
	}
	runs.wait.Wait()
}
