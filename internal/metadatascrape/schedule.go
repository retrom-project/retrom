package metadatascrape

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/hasheous"
	schedulepersistence "retrom/internal/persistence/metadatascrape"
	scheduleservice "retrom/internal/service/metadatascrape"
)

type Service struct {
	database *sql.DB
	blobs    *blobstore.Store
	provider *hasheous.Provider
	now      func() time.Time
}

var (
	errGameVersionConflict  = scheduleservice.ErrGameVersionConflict
	errGameDeleted          = scheduleservice.ErrGameDeleted
	errInitialItemState     = errors.New("initial review item state changed")
	errInitialProgressState = errors.New("initial import progress state changed")
)

type Scheduled = scheduleservice.Scheduled

func New(database *sql.DB, blobs *blobstore.Store, provider *hasheous.Provider, now func() time.Time) *Service {
	return &Service{database: database, blobs: blobs, provider: provider, now: now}
}

func (service *Service) ScheduleImport(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, provider string,
) (Scheduled, error) {
	result, err := scheduleservice.NewScheduler(
		nil,
		nil,
		service.now,
	).ScheduleImport(
		ctx,
		schedulepersistence.BindSchedule(
			transaction,
		),
		itemID,
		provider,
	)
	if err != nil {
		return Scheduled{}, fmt.Errorf("schedule initial metadata scrape: %w", err)
	}
	return result, nil
}
