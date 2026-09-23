package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/dbexec"
	librarypersistence "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

type reviewBulkWork struct{ bulkID, jobID, workerID, userID string }

func (service *Service) claimReviewBulk(ctx context.Context, bulkID string) (reviewBulkWork, error) {
	workerID, err := uuid.NewV7()
	if err != nil {
		return reviewBulkWork{}, fmt.Errorf("allocate review bulk worker: %w", err)
	}
	var jobID, userID string
	err = librarypersistence.NewTransactions(service.database).Write(ctx, func(executor dbexec.Executor) error {
		var claimErr error
		jobID, userID, claimErr = librarypersistence.BindReviewBulkWorker(executor).Claim(
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

func (service *Service) runReviewBulkApproval(ctx context.Context, bulkID string) {
	work, err := service.claimReviewBulk(ctx, bulkID)
	if errors.Is(err, librarypersistence.ErrReviewBulkNotRunnable) {
		return
	}
	if err != nil {
		return
	}
	ctx = authn.WithPrincipal(ctx, authn.Principal{UserID: work.userID, Role: "ADMIN"})
	for {
		completed, err := service.processNextReviewBulkItem(ctx, work)
		if err != nil {
			service.failReviewBulk(ctx, work)
			return
		}
		if completed {
			return
		}
	}
}

func (service *Service) processNextReviewBulkItem(ctx context.Context, work reviewBulkWork) (bool, error) {
	var completed bool
	var failedItemID string
	err := librarypersistence.NewReviewApprovals(service.database).WithBulkApprovalStep(
		ctx, func(executor dbexec.Executor, approval application.ReviewApprovalScope) error {
			worker := librarypersistence.BindReviewBulkWorker(executor)
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
			if item.ReviewUpdatedAtMS > item.CreatedAtMS || item.ItemUpdatedAtMS > item.CreatedAtMS {
				return worker.Skip(ctx, work.bulkID, work.jobID, work.workerID, item.ID, "CHANGED", now)
			}
			raw, candidateErr := librarypersistence.BindReviewBulkQueries(executor).CandidateByID(ctx, item.ID)
			if errors.Is(candidateErr, sql.ErrNoRows) {
				return worker.Skip(ctx, work.bulkID, work.jobID, work.workerID, item.ID, "NOT_READY", now)
			}
			if candidateErr != nil {
				return fmt.Errorf("read review bulk candidate: %w", candidateErr)
			}
			candidates := reviewBulkCandidatesFromApplication([]application.ReviewBulkCandidate{raw})
			qualified, counts, classifyErr := service.classifyReviewBulkCandidates(ctx, executor, candidates)
			if classifyErr != nil {
				return classifyErr
			}
			if len(qualified) == 0 {
				return worker.Skip(ctx, work.bulkID, work.jobID, work.workerID, item.ID,
					reviewBulkSkipOutcome(counts), now)
			}
			selected := qualified[0]
			_, approveErr := service.reviewApprovals().ApproveInScope(ctx, approval, application.ReviewApprovalRequest{
				ItemID: item.ID, ExpectedVersion: item.ReviewVersion,
				Bulk: &application.BulkPublicationIntent{
					BulkID: work.bulkID, JobID: work.jobID, WorkerID: work.workerID,
					ValidationID: selected.validationID.String, SourceSnapshotID: selected.sourceSnapshotID,
				},
			})
			if approveErr != nil {
				return fmt.Errorf("approve review bulk item: %w", approveErr)
			}
			return nil
		})
	if err == nil {
		return completed, nil
	}
	var duplicate *DuplicateConflict
	if errors.As(err, &duplicate) {
		return false, service.skipFailedReviewBulkApproval(ctx, work, failedItemID, "DUPLICATE")
	}
	if errors.Is(err, ErrInvalid) {
		return false, service.skipFailedReviewBulkApproval(ctx, work, failedItemID, "NOT_READY")
	}
	return false, fmt.Errorf("publish review bulk item %s: %w", failedItemID, err)
}

func reviewBulkSkipOutcome(counts ReviewBulkCounts) string {
	if counts.Duplicate > 0 {
		return "DUPLICATE"
	}
	return "NOT_READY"
}

func (service *Service) skipFailedReviewBulkApproval(
	ctx context.Context, work reviewBulkWork, itemID, outcome string,
) error {
	err := librarypersistence.NewTransactions(service.database).Write(ctx, func(executor dbexec.Executor) error {
		return librarypersistence.BindReviewBulkWorker(executor).Skip(
			ctx, work.bulkID, work.jobID, work.workerID, itemID, outcome, service.now().UnixMilli())
	})
	if err != nil {
		return fmt.Errorf("skip failed review bulk item: %w", err)
	}
	return nil
}

func (service *Service) failReviewBulk(ctx context.Context, work reviewBulkWork) {
	background := context.WithoutCancel(ctx)
	_ = librarypersistence.NewTransactions(service.database).Write(background, func(executor dbexec.Executor) error {
		return librarypersistence.BindReviewBulkWorker(executor).Fail(background,
			work.bulkID, work.jobID, work.workerID, service.now().UnixMilli())
	})
}

func (service *Service) ResumeReviewBulkJobs(ctx context.Context) {
	var ids []string
	err := librarypersistence.NewTransactions(service.database).Write(ctx, func(executor dbexec.Executor) error {
		var resumeErr error
		ids, resumeErr = librarypersistence.BindReviewBulkWorker(executor).Resume(ctx, service.now().UnixMilli())
		if resumeErr != nil {
			return fmt.Errorf("resume review bulk jobs: %w", resumeErr)
		}
		return nil
	})
	if err != nil {
		return
	}
	for _, id := range ids {
		go service.runReviewBulkApproval(context.WithoutCancel(ctx), id)
	}
}
