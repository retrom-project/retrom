package importdiscard

import (
	"context"
	"errors"
	"fmt"
	model "retrom/internal/model/importdiscard"
)

func (service *Service) process(ctx context.Context, key model.Key, userID string) (bool, error) {
	if key.Kind != "IMPORT" {
		stopped, err := service.stopSource(ctx, key, userID)
		if err != nil || !stopped {
			return false, failure("process discarded content", err)
		}
		if err := service.recoverSourceLinks(ctx, key); err != nil {
			return false, failure("process discarded content", err)
		}
	}
	ids := []string{key.ID}
	if key.Kind != "IMPORT" {
		if err := service.repository.WithRead(ctx, func(records model.Reader) error {
			var err error
			ids, err = records.Children(ctx, key)
			return failure("process discarded content", err)
		}); err != nil {
			return false, failure("process discarded content", err)
		}
	}
	for _, id := range ids {
		done, err := service.discardImport(ctx, id)
		if err != nil || !done {
			return false, failure("process discarded content", err)
		}
	}
	if key.Kind != "IMPORT" {
		return service.discardSourceItems(ctx, key)
	}
	return true, nil
}

func (service *Service) batch(ctx context.Context, key model.Key) (model.Batch, error) {
	var batch model.Batch
	err := service.repository.WithRead(ctx, func(records model.Reader) error {
		var err error
		batch, err = records.Batch(ctx, key)
		return failure("process discarded content", err)
	})
	return batch, failure("process discarded content", err)
}

func (service *Service) stopSource(ctx context.Context, key model.Key, userID string) (bool, error) {
	batch, err := service.batch(ctx, key)
	if err != nil {
		return false, failure("process discarded content", err)
	}
	if batch.State == "CANCEL_REQUESTED" {
		return false, nil
	}
	if batch.State != "QUEUED" && batch.State != "RUNNING" {
		return true, nil
	}
	err = service.sources.Cancel(ctx, key.Kind, key.ID, batch.Version, reason, userID)
	if errors.Is(err, model.ErrNotCancellable) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stop source import: %w", err)
	}
	return false, nil
}

func (service *Service) discardImport(ctx context.Context, id string) (bool, error) {
	batch, err := service.batch(ctx, model.Key{Kind: "IMPORT", ID: id})
	if err != nil {
		return false, failure("process discarded content", err)
	}
	if batch.State == "CANCEL_REQUESTED" {
		return false, nil
	}
	if batch.State != "CANCELLED" && batch.State != "COMPLETED" && batch.State != "FAILED" {
		err := service.importer.CancelForDiscard(ctx, id, batch.Version)
		if errors.Is(err, model.ErrNotCancellable) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("stop import for discard: %w", err)
		}
		return false, nil
	}
	done, err := service.importer.DiscardBatchReviews(ctx, id)
	if err != nil {
		return false, fmt.Errorf("discard batch reviews: %w", err)
	}
	if !done {
		return false, nil
	}
	if err := service.importer.ReleaseDiscardedBatch(ctx, id); err != nil {
		return false, fmt.Errorf("release discarded batch: %w", err)
	}
	return true, nil
}

func (service *Service) discardSourceItems(ctx context.Context, key model.Key) (bool, error) {
	done, err := service.repository.CommitDiscardSourceItems(ctx, model.DiscardSourceItemsCommand{
		Key: key, NowMS: service.now().UnixMilli(),
	})
	return done, failure("process discarded content", err)
}
