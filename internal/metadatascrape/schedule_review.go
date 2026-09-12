package metadatascrape

import (
	"context"
	"fmt"

	schedulepersistence "retrom/internal/persistence/metadatascrape"
	scheduleservice "retrom/internal/service/metadatascrape"
)

func (service *Service) ScheduleReview(
	ctx context.Context,
	id string,
	version int64,
	provider string,
) (Scheduled, int64, error) {
	scheduler := scheduleservice.NewScheduler(schedulepersistence.NewScheduler(service.database), service, service.now)
	result, next, err := scheduler.ScheduleReview(ctx, id, version, provider)
	if err != nil {
		return Scheduled{}, 0, fmt.Errorf("request review metadata scrape: %w", err)
	}
	return result, next, nil
}

func (service *Service) ScheduleGame(ctx context.Context, id string, version int64) (Scheduled, int64, error) {
	scheduler := scheduleservice.NewScheduler(schedulepersistence.NewScheduler(service.database), service, service.now)
	result, next, err := scheduler.ScheduleGame(ctx, id, version)
	if err != nil {
		return Scheduled{}, 0, fmt.Errorf("request game metadata scrape: %w", err)
	}
	return result, next, nil
}
