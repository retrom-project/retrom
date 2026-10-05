package libraryimport

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
)

type reviewBulkWork struct{ bulkID, jobID, workerID, userID string }

func (service *ReviewBulk) claimReviewBulk(ctx context.Context, bulkID string) (reviewBulkWork, error) {
	workerID, err := uuid.NewV7()
	if err != nil {
		return reviewBulkWork{}, fmt.Errorf("allocate review bulk worker: %w", err)
	}
	var jobID, userID string
	err = service.repository.WithStep(ctx, func(scope ReviewBulkStep) error {
		var claimErr error
		jobID, userID, claimErr = scope.Worker.Claim(
			ctx, bulkID, workerID.String(), service.now().UnixMilli())
		if claimErr != nil {
			return fmt.Errorf("claim review bulk: %w", claimErr)
		}
		return nil
	})
	if err != nil {
		return reviewBulkWork{}, fmt.Errorf("claim review bulk transaction: %w", err)
	}
	return reviewBulkWork{bulkID: bulkID, jobID: jobID, workerID: workerID.String(), userID: userID}, nil
}

func (service *ReviewBulk) runReviewBulkApproval(ctx context.Context, bulkID string) {
	work, err := service.claimReviewBulk(ctx, bulkID)
	if errors.Is(err, ErrReviewBulkNotRunnable) {
		return
	}
	if err != nil {
		cleanup.Error("claim bulk approval", err)
		return
	}
	ctx = authn.WithPrincipal(ctx, authn.Principal{UserID: work.userID, Role: "ADMIN"})
	for {
		completed, err := service.processNextReviewBulkItem(ctx, work)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			cleanup.Error("execute bulk approval", err)
			service.failReviewBulk(ctx, work)
			return
		}
		if completed {
			return
		}
	}
}

func (service *ReviewBulk) processNextReviewBulkItem(ctx context.Context, work reviewBulkWork) (bool, error) {
	var completed bool
	var failedItemID string
	var request *ReviewApprovalRequest
	err := service.repository.WithStep(ctx, func(scope ReviewBulkStep) error {
		completed, failedItemID, request = false, "", nil
		approval := scope.Approval
		worker := scope.Worker
		item, found, readErr := worker.Next(ctx, work.bulkID, work.workerID)
		if readErr != nil {
			return fmt.Errorf("read next review bulk item: %w", readErr)
		}
		now := service.now().UnixMilli()
		if !found {
			if finishErr := worker.Finish(ctx, work.bulkID, work.jobID, work.workerID, now); finishErr != nil {
				return fmt.Errorf("finish review bulk: %w", finishErr)
			}
			completed = true
			return nil
		}
		failedItemID = item.ID
		publication, readErr := approval.Publications.ReadPublication(ctx, item.ID)
		if readErr != nil {
			return fmt.Errorf("read bulk publication: %w", readErr)
		}
		if publication.GameID != "" {
			request = &ReviewApprovalRequest{ItemID: item.ID, ExpectedVersion: item.ReviewVersion}
			return nil
		}
		if item.ReviewUpdatedAtMS > item.CreatedAtMS || item.ItemUpdatedAtMS > item.CreatedAtMS {
			return worker.Skip(ctx, work.bulkID, work.jobID, work.workerID, item.ID, "CHANGED", now)
		}
		candidate, found, candidateErr := scope.Candidates.CandidateByID(ctx, item.ID)
		if candidateErr != nil {
			return fmt.Errorf("read review bulk candidate: %w", candidateErr)
		}
		if !found || !automaticApprovalCandidate(candidate) {
			return worker.Skip(ctx, work.bulkID, work.jobID, work.workerID, item.ID, "NOT_READY", now)
		}
		request = &ReviewApprovalRequest{
			ItemID: item.ID, ExpectedVersion: item.ReviewVersion,
			Bulk: &BulkPublicationIntent{
				BulkID: work.bulkID, JobID: work.jobID, WorkerID: work.workerID,
				SourceSnapshotID: candidate.SourceSnapshotID,
			},
		}

		return nil
	})
	if err == nil && request != nil {
		err = service.publishReviewBulkRequest(ctx, work, *request)
	}
	if err == nil {
		return completed, nil
	}
	return false, service.reviewBulkFailure(ctx, work, failedItemID, err)
}

func (service *ReviewBulk) reviewBulkFailure(ctx context.Context, work reviewBulkWork,
	failedItemID string, err error,
) error {
	var duplicate *DuplicateConflict
	if errors.As(err, &duplicate) {
		return service.skipFailedReviewBulkApproval(ctx, work, failedItemID, "DUPLICATE")
	}
	if errors.Is(err, ErrInvalid) {
		return service.skipFailedReviewBulkApproval(ctx, work, failedItemID, "NOT_READY")
	}
	return fmt.Errorf("publish review bulk item %s: %w", failedItemID, err)
}

func (service *ReviewBulk) skipFailedReviewBulkApproval(
	ctx context.Context, work reviewBulkWork, itemID, outcome string,
) error {
	err := service.repository.WithStep(ctx, func(scope ReviewBulkStep) error {
		return scope.Worker.Skip(
			ctx, work.bulkID, work.jobID, work.workerID, itemID, outcome, service.now().UnixMilli())
	})
	if err != nil {
		return fmt.Errorf("skip failed review bulk item: %w", err)
	}
	return nil
}

func (service *ReviewBulk) failReviewBulk(ctx context.Context, work reviewBulkWork) {
	background := context.WithoutCancel(ctx)
	err := service.repository.WithStep(background, func(scope ReviewBulkStep) error {
		return scope.Worker.Fail(background,
			work.bulkID, work.jobID, work.workerID, service.now().UnixMilli())
	})
	cleanup.Error("fail bulk approval", err)
}

func (service *ReviewBulk) Start(ctx context.Context) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.closed {
		return ErrReviewBulkConflict
	}
	if service.started {
		return nil
	}
	var ids []string
	err := service.repository.WithStep(ctx, func(scope ReviewBulkStep) error {
		var resumeErr error
		ids, resumeErr = scope.Recovery.Resume(ctx, service.now().UnixMilli())
		if resumeErr != nil {
			return fmt.Errorf("resume review bulk jobs: %w", resumeErr)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("start bulk approvals: %w", err)
	}
	service.started = true
	for _, id := range ids {
		service.launchLocked(id)
	}
	return nil
}

func (service *ReviewBulk) publishReviewBulkRequest(ctx context.Context, work reviewBulkWork,
	request ReviewApprovalRequest,
) error {
	result, err := service.approvals.Approve(ctx, request)
	if err != nil {
		return fmt.Errorf("approve bulk item: %w", err)
	}
	err = service.repository.WithStep(ctx,
		func(step ReviewBulkStep) error {
			scope := step.Approval
			now := service.now().UnixMilli()
			if err := scope.Bulk.RecordPublished(ctx, BulkPublication{
				Intent: BulkPublicationIntent{BulkID: work.bulkID, JobID: work.jobID, WorkerID: work.workerID},
				ItemID: request.ItemID, Result: result, NowMS: now,
				ReviewVersion: request.ExpectedVersion, LeasedUntilMS: now + 60_000,
			}); err != nil {
				return fmt.Errorf("record bulk publication: %w", err)
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("commit bulk progress: %w", err)
	}
	return nil
}

// Automatic approval only excludes cases requiring a human decision. Approve owns
// all shared content, dependency, validation and duplicate checks.
func automaticApprovalCandidate(candidate ReviewBulkCandidate) bool {
	return candidate.ValidationStatus != nil &&
		*candidate.ValidationStatus == "READY" && !candidate.AttachmentActive && !candidate.SourceFlagged
}
