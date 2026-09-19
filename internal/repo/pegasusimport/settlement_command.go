package pegasusimport

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	application "retrom/internal/model/pegasusimport"
	librarymodel "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
	libraryrepo "retrom/internal/repo/libraryimport"
)

func (repository *WorkerSettlement) CurrentSettlement(ctx context.Context, jobID string) (application.ExecutionSnapshot, error) {
	conn, err := repository.database.Conn(ctx)
	if err != nil {
		return application.ExecutionSnapshot{}, fmt.Errorf("acquire Pegasus settlement read: %w", err)
	}
	defer conn.Close()
	return leaseRecords{tx: conn}.Current(ctx, jobID)
}

func (repository *WorkerSettlement) CommitSettlementReviewBatch(
	ctx context.Context,
	id application.ExecutionIdentity,
	nowFunc func() int64,
	releaseYearMax int,
) (application.SettlementReviewBatchResult, error) {
	var result application.SettlementReviewBatchResult
	err := dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := workerSettlementRecords{tx: executor}
		before, err := records.Current(ctx, id.JobID)
		if err != nil {
			return fmt.Errorf("read Pegasus settlement: %w", err)
		}
		if before.Kind != "SERVER_PEGASUS_IMPORT" {
			result.Before = before
			return nil
		}
		reviews, err := recoveryRecords{tx: executor}.Reviews(ctx, before.ImportID, 101)
		if err != nil {
			return fmt.Errorf("read Pegasus settlement reviews: %w", err)
		}
		for _, review := range reviews[:min(len(reviews), 100)] {
			current, err := records.Current(ctx, id.JobID)
			if err != nil {
				return fmt.Errorf("re-read Pegasus settlement: %w", err)
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

			now := nowFunc()
			auditID, _ := uuid.NewV7()
			label := "release-setup"
			input := librarymodel.MetadataSeedInput{
				ItemID: review.Identity.LibraryItemID, Metadata: review.Metadata,
				MaximumYear: releaseYearMax, NowMS: now,
				AuditID: auditID.String(), ActorKind: "SYSTEM", ActorLabel: &label,
			}
			_, warnings, err := libraryrepo.SeedMetadata(ctx, executor, input)
			if err != nil {
				return fmt.Errorf("seed Pegasus settlement review metadata: %w", err)
			}
			change := application.RecoveryReviewChange{
				Execution: current,
				Handoff: application.ReviewHandoffChange{
					Before:   review,
					Warnings: application.MergeReviewMetadataWarnings(review.Warnings, warnings),
					NowMS:    now,
				},
			}
			settlement := workerSettlementRecords{tx: executor}
			if err := settlement.CompleteReview(ctx, change); err != nil {
				return fmt.Errorf("retain Pegasus review on settlement: %w", err)
			}
		}
		current, err := records.Current(ctx, id.JobID)
		if err != nil {
			return fmt.Errorf("re-read Pegasus settlement after reviews: %w", err)
		}
		result.Before = current
		result.More = len(reviews) > 100
		return nil
	})
	if err != nil {
		return application.SettlementReviewBatchResult{}, fmt.Errorf("commit Pegasus settlement review batch: %w", err)
	}
	return result, nil
}

func (repository *WorkerSettlement) CommitSettlement(
	ctx context.Context,
	change application.WorkerSettlementChange,
) error {
	err := dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := workerSettlementRecords{tx: executor}
		if err := records.Close(ctx, change); err != nil {
			return fmt.Errorf("persist Pegasus worker settlement: %w", err)
		}
		if err := scheduleTerminalPayloads(ctx, executor, change.Before.ImportID, change.NowMS); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit Pegasus settlement: %w", err)
	}
	return nil
}
