package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"retrom/internal/capability/security/authn"
	librarypersistence "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"

	"github.com/google/uuid"
)

var errReviewBulkNotRunnable = application.ErrReviewBulkWorkerNotRunnable

type reviewBulkWork struct {
	bulkID, jobID, workerID, userID string
}

type reviewBulkWorkItem struct {
	itemID, validationID, sourceSnapshotID string
	reviewVersion                          int64
}

type ReviewBulkItemPage struct {
	Items      []ReviewBulkItemResult `json:"items"`
	NextCursor *string                `json:"nextCursor"`
}

func (service *Service) reviewBulkWorkerRepository() application.ReviewBulkWorkerRepository {
	return librarypersistence.NewReviewBulkWorker(service.database)
}

func applicationReviewBulkWork(work reviewBulkWork) application.ReviewBulkWork {
	return application.ReviewBulkWork{
		BulkApprovalID: work.bulkID, JobID: work.jobID, WorkerID: work.workerID, UserID: work.userID,
	}
}

func reviewBulkWorkFromApplication(work application.ReviewBulkWork) reviewBulkWork {
	return reviewBulkWork{
		bulkID: work.BulkApprovalID, jobID: work.JobID, workerID: work.WorkerID, userID: work.UserID,
	}
}

func applicationReviewBulkWorkItem(item reviewBulkWorkItem) application.ReviewBulkWorkItem {
	return application.ReviewBulkWorkItem{
		ImportItemID: item.itemID, ValidationID: item.validationID,
		SourceSnapshotID: item.sourceSnapshotID, ExpectedReviewVersion: item.reviewVersion,
	}
}

func reviewBulkWorkItemFromApplication(item application.ReviewBulkWorkItem) reviewBulkWorkItem {
	return reviewBulkWorkItem{
		itemID: item.ImportItemID, validationID: item.ValidationID,
		sourceSnapshotID: item.SourceSnapshotID, reviewVersion: item.ExpectedReviewVersion,
	}
}

func (service *Service) claimReviewBulk(ctx context.Context, bulkID string) (reviewBulkWork, error) {
	workerID, _ := uuid.NewV7()
	now := service.now().UnixMilli()
	var claimed application.ReviewBulkWork
	err := service.reviewBulkWorkerRepository().WithWorker(ctx, func(scope application.ReviewBulkWorkerScope) error {
		var err error
		claimed, err = scope.Claim(ctx, application.ReviewBulkClaim{
			BulkApprovalID: bulkID, WorkerID: workerID.String(), NowMS: now,
			DeadlineMS: now + int64(reviewBulkDeadline/time.Millisecond),
		})
		if err != nil {
			return fmt.Errorf("claim review bulk work: %w", err)
		}
		return nil
	})
	if err != nil {
		return reviewBulkWork{}, fmt.Errorf("libraryimport/review bulk claim: %w", err)
	}
	return reviewBulkWorkFromApplication(claimed), nil
}

func (service *Service) claimReviewBulkItem(
	ctx context.Context,
	work reviewBulkWork,
) (reviewBulkWorkItem, error) {
	now := service.now().UnixMilli()
	var claimed application.ReviewBulkWorkItem
	err := service.reviewBulkWorkerRepository().WithWorker(ctx, func(scope application.ReviewBulkWorkerScope) error {
		var err error
		claimed, err = scope.ClaimItem(ctx, applicationReviewBulkWork(work), now)
		if err != nil {
			return fmt.Errorf("claim review bulk item: %w", err)
		}
		return nil
	})
	if err != nil {
		return reviewBulkWorkItem{}, fmt.Errorf("libraryimport/review bulk item claim: %w", err)
	}
	return reviewBulkWorkItemFromApplication(claimed), nil
}

func validReviewBulkOutcome(state string) bool {
	switch state {
	case "SKIPPED_DUPLICATE", "SKIPPED_CHANGED", "SKIPPED_NOT_READY", "FAILED_FINAL":
		return true
	default:
		return false
	}
}

func (service *Service) completeReviewBulkItem(
	ctx context.Context,
	work reviewBulkWork,
	item reviewBulkWorkItem,
	state, code string,
) error {
	if !validReviewBulkOutcome(state) {
		return ErrInvalid
	}
	details, _ := json.Marshal(map[string]any{"schemaVersion": 1, "code": code})
	now := service.now().UnixMilli()
	err := service.reviewBulkWorkerRepository().WithWorker(ctx, func(scope application.ReviewBulkWorkerScope) error {
		return scope.CompleteItem(ctx, application.ReviewBulkItemCompletion{
			Work: applicationReviewBulkWork(work), Item: applicationReviewBulkWorkItem(item),
			State: state, OutcomeCode: code, DetailsJSON: string(details), NowMS: now,
			LeasedUntilMS: now + 60_000,
		})
	})
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk outcome: %w", err)
	}
	return nil
}

func (service *Service) reviewBulkItemStillFrozen(
	ctx context.Context,
	item reviewBulkWorkItem,
) bool {
	var frozen bool
	err := service.reviewBulkWorkerRepository().WithWorker(ctx, func(scope application.ReviewBulkWorkerScope) error {
		var err error
		frozen, err = scope.ItemStillFrozen(ctx, applicationReviewBulkWorkItem(item))
		if err != nil {
			return fmt.Errorf("read review bulk item state: %w", err)
		}
		return nil
	})
	return err == nil && frozen
}

func (service *Service) processReviewBulkItem(
	ctx context.Context,
	work reviewBulkWork,
	item reviewBulkWorkItem,
) error {
	_, err := service.reviewApprovals().Approve(ctx, application.ReviewApprovalRequest{
		ItemID: item.itemID, ExpectedVersion: item.reviewVersion,
		Bulk: &application.BulkPublicationIntent{
			BulkID: work.bulkID, JobID: work.jobID, WorkerID: work.workerID,
			ValidationID: item.validationID, SourceSnapshotID: item.sourceSnapshotID,
		},
	})
	if err == nil {
		return nil
	}
	var duplicate *DuplicateConflict
	if errors.As(err, &duplicate) {
		return service.completeReviewBulkItem(
			ctx, work, item, "SKIPPED_DUPLICATE", "DUPLICATE_GAME_CONFIRMATION_REQUIRED",
		)
	}
	if errors.Is(err, ErrInvalid) {
		if service.reviewBulkItemStillFrozen(ctx, item) {
			return service.completeReviewBulkItem(ctx, work, item, "SKIPPED_NOT_READY", "REVIEW_NOT_STRICT_READY")
		}
		return service.completeReviewBulkItem(ctx, work, item, "SKIPPED_CHANGED", "REVIEW_INPUT_CHANGED")
	}
	return service.completeReviewBulkItem(ctx, work, item, "FAILED_FINAL", "REVIEW_BULK_ITEM_FAILED")
}

func (service *Service) finishReviewBulk(ctx context.Context, work reviewBulkWork) error {
	now := service.now().UnixMilli()
	err := service.reviewBulkWorkerRepository().WithWorker(ctx, func(scope application.ReviewBulkWorkerScope) error {
		if err := scope.Finish(ctx, applicationReviewBulkWork(work), now); err != nil {
			return fmt.Errorf("finish review bulk work: %w", err)
		}
		return nil
	})
	if errors.Is(err, application.ErrReviewBulkWorkerNotRunnable) {
		return fmt.Errorf("libraryimport/review bulk finish: %w", errReviewBulkNotRunnable)
	}
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk finish: %w", err)
	}
	return nil
}

func (service *Service) finalizeReviewBulkCancellation(ctx context.Context, bulkID string) error {
	now := service.now().UnixMilli()
	err := service.reviewBulkWorkerRepository().WithWorker(ctx, func(scope application.ReviewBulkWorkerScope) error {
		if err := scope.FinalizeCancellation(ctx, bulkID, now); err != nil {
			return fmt.Errorf("finalize review bulk cancellation: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("libraryimport/review bulk cancel: %w", err)
	}
	return nil
}

func (service *Service) reviewBulkCancellationRequested(ctx context.Context, work reviewBulkWork) bool {
	var requested bool
	err := service.reviewBulkWorkerRepository().WithWorker(ctx, func(scope application.ReviewBulkWorkerScope) error {
		var err error
		requested, err = scope.CancellationRequested(ctx, work.bulkID)
		if err != nil {
			return fmt.Errorf("read review bulk cancellation: %w", err)
		}
		return nil
	})
	return err == nil && requested
}

func (service *Service) failReviewBulkWorker(ctx context.Context, work reviewBulkWork) {
	now := service.now().UnixMilli()
	_ = service.reviewBulkWorkerRepository().WithWorker(ctx, func(scope application.ReviewBulkWorkerScope) error {
		return scope.Fail(ctx, applicationReviewBulkWork(work), now)
	})
}

func (service *Service) failQueuedReviewBulkWorker(ctx context.Context, bulkID string) {
	now := service.now().UnixMilli()
	_ = service.reviewBulkWorkerRepository().WithWorker(ctx, func(scope application.ReviewBulkWorkerScope) error {
		return scope.FailQueued(ctx, bulkID, now)
	})
}

func (service *Service) runReviewBulkApproval(ctx context.Context, bulkID string) {
	work, err := service.claimReviewBulk(ctx, bulkID)
	if errors.Is(err, errReviewBulkNotRunnable) {
		return
	}
	if err != nil {
		service.failQueuedReviewBulkWorker(context.WithoutCancel(ctx), bulkID)
		return
	}
	ctx = authn.WithPrincipal(ctx, authn.Principal{UserID: work.userID, Role: "ADMIN"})
	for {
		if service.reviewBulkCancellationRequested(ctx, work) {
			_ = service.finalizeReviewBulkCancellation(ctx, work.bulkID)
			return
		}
		item, err := service.claimReviewBulkItem(ctx, work)
		if errors.Is(err, sql.ErrNoRows) {
			service.finishOrCancelReviewBulk(ctx, work)
			return
		}
		if err != nil {
			service.failReviewBulkWorker(ctx, work)
			return
		}
		if err := service.processReviewBulkItem(ctx, work, item); err != nil {
			service.cancelOrFailReviewBulk(ctx, work)
			return
		}
	}
}

func (service *Service) cancelOrFailReviewBulk(ctx context.Context, work reviewBulkWork) {
	if service.reviewBulkCancellationRequested(ctx, work) {
		_ = service.finalizeReviewBulkCancellation(ctx, work.bulkID)
		return
	}
	service.failReviewBulkWorker(ctx, work)
}

func (service *Service) finishOrCancelReviewBulk(ctx context.Context, work reviewBulkWork) {
	if service.reviewBulkCancellationRequested(ctx, work) {
		_ = service.finalizeReviewBulkCancellation(ctx, work.bulkID)
		return
	}
	if err := service.finishReviewBulk(ctx, work); err != nil {
		service.cancelOrFailReviewBulk(ctx, work)
	}
}

func (service *Service) ResumeReviewBulkJobs(ctx context.Context) {
	now := service.now().UnixMilli()
	var values []application.ReviewBulkResumableJob
	err := service.reviewBulkWorkerRepository().WithWorker(ctx, func(scope application.ReviewBulkWorkerScope) error {
		var err error
		values, err = scope.Resume(ctx, now)
		if err != nil {
			return fmt.Errorf("resume review bulk work: %w", err)
		}
		return nil
	})
	if err != nil {
		return
	}
	for _, value := range values {
		if value.State == "CANCEL_REQUESTED" {
			_ = service.finalizeReviewBulkCancellation(context.WithoutCancel(ctx), value.ID)
			continue
		}
		go service.runReviewBulkApproval(context.WithoutCancel(ctx), value.ID)
	}
}

func (service *Service) CancelReviewBulk(
	ctx context.Context,
	bulkID string,
	expectedVersion int64,
	reason string,
) (ReviewBulkSummary, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return ReviewBulkSummary{}, ErrReviewBulkConflict
	}
	var target application.ReviewBulkCancelTarget
	err := service.reviewBulkWorkerRepository().WithWorker(ctx, func(scope application.ReviewBulkWorkerScope) error {
		var err error
		target, err = scope.LoadCancelTarget(ctx, bulkID, expectedVersion)
		if err != nil {
			return fmt.Errorf("load review bulk cancellation target: %w", err)
		}
		if err := scope.RequestCancellation(ctx, application.ReviewBulkCancellationRequest{
			Target: target, BulkApprovalID: bulkID, Reason: reason,
			ExpectedVersion: expectedVersion, NowMS: service.now().UnixMilli(),
		}); err != nil {
			return fmt.Errorf("request review bulk cancellation: %w", err)
		}
		return nil
	})
	if errors.Is(err, application.ErrReviewBulkWorkerNotRunnable) {
		return ReviewBulkSummary{}, ErrReviewBulkConflict
	}
	if err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk cancel: %w", err)
	}
	if target.State == "QUEUED" {
		if err := service.finalizeReviewBulkCancellation(ctx, bulkID); err != nil {
			return ReviewBulkSummary{}, err
		}
	}
	return service.GetReviewBulk(ctx, bulkID)
}

func (service *Service) RetryReviewBulk(
	ctx context.Context,
	bulkID string,
	expectedVersion int64,
) (ReviewBulkSummary, error) {
	var target application.ReviewBulkRetryTarget
	err := service.reviewBulkWorkerRepository().WithWorker(ctx, func(scope application.ReviewBulkWorkerScope) error {
		var err error
		target, err = scope.LoadRetryTarget(ctx, bulkID, expectedVersion)
		if err != nil {
			return fmt.Errorf("load review bulk retry target: %w", err)
		}
		active, err := scope.Active(ctx)
		if err != nil {
			return fmt.Errorf("read active review bulk worker: %w", err)
		}
		if active {
			return ErrReviewBulkActive
		}
		if err := scope.QueueRetry(ctx, application.ReviewBulkRetryRequest{
			Target: target, BulkApprovalID: bulkID, ExpectedVersion: expectedVersion,
			NowMS: service.now().UnixMilli(),
		}); err != nil {
			return fmt.Errorf("queue review bulk retry: %w", err)
		}
		return nil
	})
	if errors.Is(err, application.ErrReviewBulkWorkerNotRunnable) {
		return ReviewBulkSummary{}, ErrReviewBulkConflict
	}
	if err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk retry: %w", err)
	}
	go service.runReviewBulkApproval(context.WithoutCancel(ctx), bulkID)
	return service.GetReviewBulk(ctx, bulkID)
}
