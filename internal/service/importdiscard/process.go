package importdiscard

import (
	"context"
	"errors"
	"fmt"
)

func (service *Service) process(ctx context.Context, key Key, userID string) (bool, error) {
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
		if err := service.repository.WithRead(ctx, func(records Reader) error {
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

func (service *Service) batch(ctx context.Context, key Key) (Batch, error) {
	var batch Batch
	err := service.repository.WithRead(ctx, func(records Reader) error {
		var err error
		batch, err = records.Batch(ctx, key)
		return failure("process discarded content", err)
	})
	return batch, failure("process discarded content", err)
}

func (service *Service) stopSource(ctx context.Context, key Key, userID string) (bool, error) {
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
	if errors.Is(err, ErrNotCancellable) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stop source import: %w", err)
	}
	return false, nil
}

func (service *Service) discardImport(ctx context.Context, id string) (bool, error) {
	batch, err := service.batch(ctx, Key{Kind: "IMPORT", ID: id})
	if err != nil {
		return false, failure("process discarded content", err)
	}
	if batch.State == "CANCEL_REQUESTED" {
		return false, nil
	}
	if batch.State != "CANCELLED" && batch.State != "COMPLETED" && batch.State != "FAILED" {
		err := service.importer.CancelForDiscard(ctx, id, batch.Version)
		if errors.Is(err, ErrNotCancellable) {
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

func (service *Service) discardSourceItems(ctx context.Context, key Key) (bool, error) {
	var done bool
	now := service.now().UnixMilli()
	err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
		releases, err := scope.Sources.Releases(ctx, key)
		if err != nil {
			return failure("process discarded content", err)
		}
		if releases.Releasing > 0 {
			if releases.Failed > 0 {
				return ErrReleaseFailed
			}
			return nil
		}
		ids, err := scope.Sources.UnusedUploads(ctx, key)
		if err != nil {
			return failure("process discarded content", err)
		}
		for _, id := range ids {
			if err := scope.Sources.DeleteUpload(ctx, id); err != nil {
				return failure("process discarded content", err)
			}
		}
		if err := scope.Sources.Complete(ctx, key, now); err != nil {
			return failure("process discarded content", err)
		}
		done = true
		return nil
	})
	return done, failure("process discarded content", err)
}
