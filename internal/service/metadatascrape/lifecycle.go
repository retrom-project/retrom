package metadatascrape

import (
	"context"
	"fmt"
	"time"

	"retrom/internal/cleanup"
)

func (service *executionSupervisor) register(
	parent context.Context, id string, execution bool,
) (context.Context, func(), error) {
	service.mutex.Lock()
	defer service.mutex.Unlock()
	if service.closed || service.stopping.Load() || service.runner == nil {
		return nil, nil, ErrWorkerClosed
	}
	if service.active[id] != nil || execution && service.executing >= 2 {
		return nil, nil, ErrExecutionBusy
	}
	if execution {
		service.executing++
	}
	ctx, cancel := context.WithCancelCause(parent)
	service.active[id] = cancel
	service.group.Add(1)
	return ctx, func() {
		cancel(nil)
		service.mutex.Lock()
		delete(service.active, id)
		if execution {
			service.executing--
		}
		service.mutex.Unlock()
		service.group.Done()
	}, nil
}

func (service *executionSupervisor) Dispatch(parent context.Context, id string) bool {
	ctx, finish, err := service.register(context.WithoutCancel(parent), id, true)
	service.Start(parent)
	if err != nil {
		return false
	}
	go func() { defer finish(); cleanup.Error("run metadata scrape", service.execute(ctx, id)) }()
	return true
}

func (service *executionSupervisor) Recover(parent context.Context) error {
	ctx, finish, err := service.register(parent, "metadata-discovery", false)
	if err != nil {
		return err
	}
	defer finish()
	return service.recover(ctx)
}

func (service *executionSupervisor) recover(ctx context.Context) error {
	ids, err := service.runner.Recover(ctx)
	if err != nil {
		return fmt.Errorf("read metadata dispatch queue: %w", err)
	}
	for _, id := range ids {
		service.Dispatch(ctx, id)
	}
	return nil
}

func (service *executionSupervisor) Start(parent context.Context) {
	service.mutex.Lock()
	if service.started || service.closed || service.runner == nil {
		service.mutex.Unlock()
		return
	}
	service.started = true
	service.mutex.Unlock()
	ctx, finish, err := service.register(context.WithoutCancel(parent), "metadata-recovery", false)
	if err != nil {
		return
	}
	go func() {
		defer finish()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			cleanup.Error("recover metadata scrape queue", service.Recover(ctx))
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (service *executionSupervisor) stop() {
	service.mutex.Lock()
	service.closed = true
	for _, cancel := range service.active {
		cancel(ErrWorkerClosed)
	}
	service.mutex.Unlock()
}
