package importdiscard

import (
	"context"
	"errors"
	"time"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
)

// Start reconciles persisted dispositions, including requests interrupted by a restart.
// Work is bounded; the existing import jobs and PAYLOAD_RELEASE jobs retain their own lifecycle.
func (service *Service) Start() {
	service.wait.Add(1)
	go func() {
		defer service.wait.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-service.stop:
				return
			case <-ticker.C:
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, err := service.RunOnce(ctx)
			cleanup.Error("reconcile import batch discard", err)
			cancel()
		}
	}()
}

func (service *Service) Close() { close(service.stop); service.wait.Wait() }

func (service *Service) RunOnce(ctx context.Context) (bool, error) {
	var request Request
	var found bool
	err := service.repository.WithRead(ctx, func(records Reader) error {
		var err error
		request, found, err = records.Pending(ctx)
		return failure("reconcile discard request", err)
	})
	if err != nil {
		return false, failure("reconcile discard request", err)
	}
	if !found {
		return false, nil
	}
	ctx = authn.WithPrincipal(ctx, authn.Principal{UserID: request.UserID})
	done, workErr := service.process(ctx, request.Key, request.UserID)
	progress := progressFor(request.Key, done, workErr, service.now().UnixMilli())
	// Cancellation leaves a durable REQUESTED disposition for the next reconciliation.
	if ctx.Err() != nil {
		return true, errors.Join(workErr, ctx.Err())
	}
	err = service.repository.WithWrite(ctx, func(scope WriteScope) error {
		return scope.Requests.Progress(ctx, progress)
	})
	return true, errors.Join(workErr, err)
}

func progressFor(key Key, done bool, workErr error, now int64) Progress {
	result := Progress{Key: key, State: "REQUESTED", Now: now}
	if done {
		result.State = "COMPLETED"
		result.CompletedAt = &now
	}
	if workErr != nil {
		result.State = "FAILED"
		result.CompletedAt = nil
		code := "IMPORT_BATCH_DISCARD_FAILED"
		if errors.Is(workErr, ErrReleaseFailed) {
			code = "IMPORT_BATCH_DISCARD_RELEASE_FAILED"
		}
		result.ErrorCode = &code
	}
	return result
}
