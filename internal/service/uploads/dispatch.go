package uploads

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/telemetry"
)

func (service *Service) register(parent context.Context, id string) (context.Context, func(), bool) {
	service.mutex.Lock()
	defer service.mutex.Unlock()
	if service.closed || service.active[id] != nil {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancelCause(parent)
	service.active[id] = cancel
	service.group.Add(1)
	return ctx, func() {
		cancel(nil)
		service.mutex.Lock()
		delete(service.active, id)
		service.mutex.Unlock()
		service.group.Done()
	}, true
}

func (service *Service) Resume(parent context.Context, id string) bool {
	ctx, finish, ok := service.register(context.WithoutCancel(parent), id)
	if !ok {
		return false
	}
	// Finalization outlives its admission request and owns a separate timing scope.
	ctx, trace := telemetry.StartTrace(ctx, rand.Text(), "BACKGROUND uploads.finalize")
	go func() {
		defer finish()
		defer trace.Report(ctx)
		cleanup.Error("finalize upload", service.Run(ctx, id))
	}()
	return true
}

func (service *Service) Recover(ctx context.Context) error {
	ids, err := service.repository.Recoverable(ctx, service.now().UnixMilli())
	if err != nil {
		return fmt.Errorf("recover uploads: %w", err)
	}
	for _, id := range ids {
		service.Resume(ctx, id)
	}
	return nil
}

func (service *Service) Start(parent context.Context) {
	service.mutex.Lock()
	if service.started || service.closed {
		service.mutex.Unlock()
		return
	}
	service.started = true
	service.mutex.Unlock()
	ctx, finish, ok := service.register(context.WithoutCancel(parent), "upload-recovery")
	if !ok {
		return
	}
	go func() {
		defer finish()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			cleanup.Error("recover upload finalization", service.Recover(ctx))
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (service *Service) Close() {
	service.Stop()
	service.Wait()
}

func (service *Service) Stop() {
	service.mutex.Lock()
	service.closed = true
	for _, cancel := range service.active {
		cancel(ErrWorkerClosed)
	}
	service.mutex.Unlock()
}

func (service *Service) Wait() { service.group.Wait() }
