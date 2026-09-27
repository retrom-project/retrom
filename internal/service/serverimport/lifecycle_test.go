package serverimport

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type blockingRecovery struct {
	OutcomeRepository
	calls     atomic.Int32
	started   chan struct{}
	cancelled chan struct{}
	release   chan struct{}
}

func (repository *blockingRecovery) Recovery(ctx context.Context, _ int64) (RecoveryWork, bool, error) {
	if repository.calls.Add(1) == 1 {
		close(repository.started)
		<-ctx.Done()
		close(repository.cancelled)
		<-repository.release
	}
	return RecoveryWork{}, false, ctx.Err()
}

func TestCloseCancelsAndJoinsServerImportWorker(t *testing.T) {
	repository := &blockingRecovery{
		started: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{}),
	}
	service := New(Repositories{Outcomes: repository}, Options{Now: time.Now})
	service.Start(t.Context())
	<-repository.started
	service.Start(t.Context())
	closed := make(chan struct{})
	go func() { service.Close(); close(closed) }()
	<-repository.cancelled
	select {
	case <-closed:
		t.Error("Close returned before the active operation stopped")
	default:
	}
	close(repository.release)
	<-closed
	service.Close()
	service.Start(t.Context())
	if repository.calls.Load() != 1 {
		t.Fatalf("worker was started more than once: %d", repository.calls.Load())
	}
}
