package pegasusimport

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	librarymodel "retrom/internal/model/libraryimport"
	application "retrom/internal/model/pegasusimport"
	"retrom/internal/repo/dbexec"
	libraryrepo "retrom/internal/repo/libraryimport"
)

func (repository *Recovery) CurrentRecovery(ctx context.Context, jobID string) (application.RecoverySnapshot, error) {
	conn, err := repository.database.Conn(ctx)
	if err != nil {
		return application.RecoverySnapshot{}, fmt.Errorf("acquire Pegasus recovery read: %w", err)
	}
	defer conn.Close()
	return recoveryRecords{tx: conn}.Current(ctx, jobID)
}

func (repository *Recovery) CommitRecoveryReviewBatch(
	ctx context.Context,
	id application.ExecutionIdentity,
	nowMS int64,
	releaseYearMax int,
) (application.RecoveryReviewBatchResult, error) {
	var result application.RecoveryReviewBatchResult
	err := dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := recoveryRecords{tx: executor}
		before, err := records.Current(ctx, id.JobID)
		if err != nil {
			return fmt.Errorf("read Pegasus recovery: %w", err)
		}
		if before.Kind != "SERVER_PEGASUS_IMPORT" {
			result.Before = before
			return nil
		}
		reviews, err := records.Reviews(ctx, before.ImportID, 101)
		if err != nil {
			return fmt.Errorf("read Pegasus recovery reviews: %w", err)
		}
		for _, review := range reviews[:min(len(reviews), 100)] {
			current, err := records.Current(ctx, id.JobID)
			if err != nil {
				return fmt.Errorf("re-read Pegasus recovery: %w", err)
			}
			review.ImportVersion = current.ImportVersion

			if review.Identity.JobID != current.JobID || review.Identity.ImportID != current.ImportID ||
				review.Identity.ExecutionNo != current.ExecutionNo || review.Identity.Attempt != current.Attempt ||
				review.Identity.LibraryItemID == "" || review.Identity.LibraryJobID == "" ||
				review.Version < 1 {
				return application.ErrVersionConflict
			}
			if review.State != "PENDING" && review.State != "COPYING" && review.State != "VALIDATING" {
				return application.ErrVersionConflict
			}

			auditID, _ := uuid.NewV7()
			label := "release-setup"
			input := librarymodel.MetadataSeedInput{
				ItemID: review.Identity.LibraryItemID, Metadata: review.Metadata,
				MaximumYear: releaseYearMax, NowMS: nowMS,
				AuditID: auditID.String(), ActorKind: "SYSTEM", ActorLabel: &label,
			}
			_, warnings, err := libraryrepo.SeedMetadata(ctx, executor, input)
			if err != nil {
				return fmt.Errorf("seed Pegasus recovery review metadata: %w", err)
			}
			change := application.RecoveryReviewChange{
				Execution: current,
				Handoff: application.ReviewHandoffChange{
					Before:   review,
					Warnings: application.MergeReviewMetadataWarnings(review.Warnings, warnings),
					NowMS:    nowMS,
				},
			}
			if err := records.CompleteReview(ctx, change); err != nil {
				return fmt.Errorf("retain Pegasus review on recovery: %w", err)
			}
		}
		current, err := records.Current(ctx, id.JobID)
		if err != nil {
			return fmt.Errorf("re-read Pegasus recovery after reviews: %w", err)
		}
		result.Before = current
		result.More = len(reviews) > 100
		return nil
	})
	if err != nil {
		return application.RecoveryReviewBatchResult{}, fmt.Errorf("commit Pegasus recovery review batch: %w", err)
	}
	return result, nil
}

func (repository *Recovery) CommitRecovery(ctx context.Context, change application.RecoveryChange) error {
	err := dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := recoveryRecords{tx: executor}
		if err := records.Apply(ctx, change); err != nil {
			return fmt.Errorf("persist Pegasus recovery: %w", err)
		}
		if change.JobState != "QUEUED" {
			return scheduleTerminalPayloads(ctx, executor, change.Before.ImportID, change.NowMS)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit Pegasus recovery: %w", err)
	}
	return nil
}
