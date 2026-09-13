package libraryimport

import (
	"context"
	"errors"
	"fmt"

	librarycomposition "retrom/internal/bootstrap/composition/libraryimport"
	librarypersistence "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

type DecisionResult = libraryservice.ReviewDecisionResult

func (service *Service) reviewDiscards() *libraryservice.ReviewDiscards {
	return libraryservice.NewReviewDiscards(librarypersistence.NewReviewDiscards(service.database), service.now)
}

func (service *Service) Discard(
	ctx context.Context, itemID string, expectedVersion int64, reason string,
) (DecisionResult, error) {
	result, err := service.reviewDiscards().Discard(ctx, libraryservice.ReviewDiscardRequest{
		ItemID: itemID, ExpectedVersion: expectedVersion, Reason: reason, Mode: libraryservice.ReviewDiscardSingle,
	})
	if err != nil {
		return DecisionResult{}, fmt.Errorf("libraryimport/discard review: %w", err)
	}
	return result, nil
}

type RetryResult struct {
	ItemID  string `json:"itemId"`
	JobID   string `json:"jobId"`
	State   string `json:"state"`
	Version int64  `json:"version"`
}

// Retry eligibility, execution creation, event emission, and aggregate update share one transaction.
func (service *Service) RetryItem(ctx context.Context, itemID string, expectedVersion int64) (RetryResult, error) {
	result, err := librarycomposition.NewImportItemRetries(service.database, service.now).Retry(ctx,
		libraryservice.ImportItemRetryRequest{ItemID: itemID, ExpectedVersion: expectedVersion})
	if err != nil {
		if errors.Is(err, libraryservice.ErrInvalid) {
			return RetryResult{}, ErrInvalid
		}
		return RetryResult{}, fmt.Errorf("libraryimport/review retry: %w", err)
	}
	return RetryResult{ItemID: result.ItemID, JobID: result.JobID, State: result.State, Version: result.Version}, nil
}

type CancelResult struct {
	ImportJobID string `json:"importJobId"`
	State       string `json:"state"`
	Version     int64  `json:"version"`
}

func (service *Service) Cancel(
	ctx context.Context,
	importID string,
	expectedVersion int64,
	reason string,
) (CancelResult, bool, error) {
	return service.cancelImport(ctx, importID, expectedVersion, reason, false)
}

// CancelForDiscard stops execution but leaves existing reviews for explicit discard events.
func (service *Service) CancelForDiscard(
	ctx context.Context, importID string, expectedVersion int64,
) (CancelResult, bool, error) {
	return service.cancelImport(ctx, importID, expectedVersion, "丢弃本批次未发布内容", true)
}

func (service *Service) cancelImport(
	ctx context.Context, importID string, expectedVersion int64, reason string, preserveReviews bool,
) (CancelResult, bool, error) {
	result, err := librarycomposition.NewImportBatchCancellations(service.database, service.now).Cancel(ctx,
		libraryservice.ImportBatchCancellationRequest{
			ImportID: importID, ExpectedVersion: expectedVersion,
			Reason: reason, PreserveReviews: preserveReviews,
		})
	if err != nil {
		if errors.Is(err, libraryservice.ErrInvalid) {
			return CancelResult{}, false, ErrInvalid
		}
		return CancelResult{}, false, fmt.Errorf("libraryimport/review cancellation: %w", err)
	}
	if result.Pending && result.GroupJobID != "" {
		service.CancelImportGroupJob(result.GroupJobID)
	}
	return CancelResult{ImportJobID: result.ImportID, State: result.State, Version: result.Version}, result.Pending, nil
}
