package metadatascrape

import (
	"context"
	"fmt"
	"time"
)

type Service struct {
	*Scheduler
	runner ScrapeRunner
}

func New(repository ScheduleRepository, runner ScrapeRunner, now func() time.Time) *Service {
	return &Service{Scheduler: NewScheduler(repository, runner, now), runner: runner}
}

func (service *Service) Run(ctx context.Context, id string) error {
	if err := service.runner.Run(ctx, id); err != nil {
		return fmt.Errorf("run metadata worker: %w", err)
	}
	return nil
}
