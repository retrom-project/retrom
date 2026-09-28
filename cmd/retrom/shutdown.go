package main

import (
	"context"
	"errors"
	"fmt"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"retrom/internal/config"
)

const processShutdownTimeout = 30 * time.Second

var errShutdownDeadline = errors.New("process shutdown deadline exceeded")

type shutdownProgress struct {
	mutex   sync.Mutex
	phase   string
	pending func() []string
}

func (progress *shutdownProgress) set(phase string, pending func() []string) {
	progress.mutex.Lock()
	defer progress.mutex.Unlock()
	progress.phase, progress.pending = phase, pending
}

func (progress *shutdownProgress) diagnostic() string {
	progress.mutex.Lock()
	phase, pending := progress.phase, progress.pending
	progress.mutex.Unlock()
	if pending == nil {
		return phase
	}
	return fmt.Sprintf("%s: %v", phase, pending())
}

func superviseServer(configuration config.Config) error {
	lifetime, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	progress := &shutdownProgress{phase: "startup"}
	return superviseShutdown(lifetime, processShutdownTimeout, progress, func(ctx context.Context, begin func()) error {
		return runServer(ctx, configuration, begin, progress)
	})
}

// Resource ownership stays inside run. If it cannot finish, main exits without running resource defers concurrently.
func superviseShutdown(parent context.Context, timeout time.Duration, progress *shutdownProgress,
	run func(context.Context, func()) error,
) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	var once sync.Once
	stopping := make(chan struct{})
	begin := func() { once.Do(func() { close(stopping) }) }
	done := make(chan error, 1)
	go func() { done <- run(ctx, begin) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		begin()
	case <-stopping:
		cancel()
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		return fmt.Errorf("%w: %s", errShutdownDeadline, progress.diagnostic())
	}
}
