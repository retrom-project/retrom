package metadatascrape

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrWorkerClosed  = errors.New("metadata worker is closed")
	ErrExecutionBusy = errors.New("metadata execution capacity is occupied")
)

type ManagedRunner interface {
	Run(context.Context, string) error
	Recover(context.Context) ([]string, error)
}

type executionSupervisor struct {
	stopping        *atomic.Bool
	runner          ManagedRunner
	mutex           sync.Mutex
	active          map[string]context.CancelCauseFunc
	group           sync.WaitGroup
	closed, started bool
	executing       int
}

type Service struct {
	*Scheduler
	scrapes, media *executionSupervisor
	stopping       atomic.Bool
}

func New(repository ScheduleRepository, runner ManagedRunner, now func() time.Time) *Service {
	return NewWithMedia(repository, runner, nil, now)
}

func NewWithMedia(repository ScheduleRepository, runner, media ManagedRunner, now func() time.Time) *Service {
	service := &Service{}
	service.scrapes = newExecutionSupervisor(runner, &service.stopping)
	service.media = newExecutionSupervisor(media, &service.stopping)
	service.Scheduler = NewScheduler(repository, service, now)
	return service
}

func newExecutionSupervisor(runner ManagedRunner, stopping *atomic.Bool) *executionSupervisor {
	return &executionSupervisor{runner: runner, stopping: stopping, active: make(map[string]context.CancelCauseFunc)}
}

func (service *Service) Run(ctx context.Context, id string) error {
	return service.scrapes.Run(ctx, id)
}

func (service *Service) Dispatch(ctx context.Context, id string) bool {
	service.media.Start(ctx)
	return service.scrapes.Dispatch(ctx, id)
}

func (service *Service) ResumeMediaJob(ctx context.Context, id string) bool {
	return service.media.Dispatch(ctx, id)
}

func (service *Service) Recover(ctx context.Context) error {
	return errors.Join(service.scrapes.Recover(ctx), service.recoverMedia(ctx))
}

func (service *Service) recoverMedia(ctx context.Context) error {
	if service.media.runner == nil {
		return nil
	}
	return service.media.Recover(ctx)
}

func (service *Service) Start(ctx context.Context) {
	service.scrapes.Start(ctx)
	service.media.Start(ctx)
}

func (service *Service) Close() {
	service.stopping.Store(true)
	service.scrapes.stop()
	service.media.stop()
	service.scrapes.group.Wait()
	service.media.group.Wait()
}

func (service *executionSupervisor) Run(ctx context.Context, id string) error {
	ctx, finish, err := service.register(ctx, id, true)
	if err != nil {
		return err
	}
	defer finish()
	return service.execute(ctx, id)
}

func (service *executionSupervisor) execute(ctx context.Context, id string) error {
	if err := service.runner.Run(ctx, id); err != nil {
		return fmt.Errorf("run metadata worker: %w", err)
	}
	return nil
}
