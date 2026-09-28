package application

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
)

type blockedWorker struct {
	stopped chan struct{}
	exited  chan struct{}
	once    sync.Once
}

func (worker *blockedWorker) Stop() { worker.once.Do(func() { close(worker.stopped) }) }
func (worker *blockedWorker) Wait() { <-worker.exited }

func TestShutdownCancelsAllWorkersAndKeepsCleanupUntilTheyExit(t *testing.T) {
	first := &blockedWorker{stopped: make(chan struct{}), exited: make(chan struct{})}
	second := &blockedWorker{stopped: make(chan struct{}), exited: make(chan struct{})}
	cleaned := make(chan struct{})
	services := &Services{shutdown: newShutdownGroup([]namedWorker{{"first", first}, {"second", second}},
		func() { close(cleaned) })}
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { result <- services.Shutdown(ctx) }()
	<-first.stopped
	<-second.stopped
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown=%v", err)
	}
	if !slices.Contains(services.PendingShutdown(), "first") {
		t.Fatal("lost blocked worker diagnostics")
	}
	select {
	case <-cleaned:
		t.Fatal("cleanup stopped before producers exited")
	default:
	}
	if err := services.Start(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatalf("restart=%v", err)
	}
	close(first.exited)
	select {
	case <-cleaned:
		t.Fatal("cleanup stopped while second producer was running")
	default:
	}
	close(second.exited)
	services.Close()
	<-cleaned
	services.Close()
	if len(services.PendingShutdown()) != 0 {
		t.Fatal("completed workers still reported pending")
	}
}

func TestCatalogCancellationIsNotCompletion(t *testing.T) {
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	task := newCatalogTask(func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-release
		return ctx.Err()
	})
	task.Start(t.Context())
	<-started
	task.Stop()
	<-cancelled
	joined := make(chan struct{})
	go func() { task.Wait(); close(joined) }()
	select {
	case <-joined:
		t.Fatal("cancel treated as completion")
	default:
	}
	close(release)
	<-joined
}
