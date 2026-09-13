package metadatascrape

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

type Service struct {
	*Scheduler
	runner    ManagedRunner
	mutex     sync.Mutex
	active    map[string]context.CancelCauseFunc
	group     sync.WaitGroup
	closed    bool
	started   bool
	executing int
}

func New(repository ScheduleRepository, runner ManagedRunner, now func() time.Time) *Service {
	service := &Service{runner: runner, active: make(map[string]context.CancelCauseFunc)}
	service.Scheduler = NewScheduler(repository, service, now)
	return service
}

func (service *Service) Run(ctx context.Context, id string) error {
	ctx, finish, err := service.register(ctx, id, true)
	if err != nil {
		return err
	}
	defer finish()
	return service.execute(ctx, id)
}

func (service *Service) execute(ctx context.Context, id string) error {
	if err := service.runner.Run(ctx, id); err != nil {
		return fmt.Errorf("run metadata worker: %w", err)
	}
	return nil
}
