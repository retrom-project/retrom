package emulationstationimport

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	application "retrom/internal/model/emulationstationimport"
	librarymodel "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
	libraryrepo "retrom/internal/repo/libraryimport"
)

func (repository *ExecutionControl) CurrentExecution(ctx context.Context, jobID string) (application.LeaseSnapshot, bool, error) {
	conn, err := repository.database.Conn(ctx)
	if err != nil {
		return application.LeaseSnapshot{}, false, fmt.Errorf("acquire EmulationStation execution read: %w", err)
	}
	defer conn.Close()
	return scanLease(conn.QueryRowContext(ctx, leaseSnapshotSQL+` AND job.id=?`, jobID))
}

func (repository *ExecutionControl) TerminalCount(ctx context.Context, importID string) (int64, error) {
	var count int64
	err := repository.database.QueryRowContext(ctx, `SELECT count(*) FROM emulationstation_import_items
WHERE import_id=? AND execution_state NOT IN ('PENDING','COPYING','VALIDATING')`, importID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count terminal EmulationStation execution items: %w", err)
	}
	return count, nil
}

func (repository *ExecutionControl) CommitExecutionReviewBatch(
	ctx context.Context,
	unit application.Execution,
	nowFunc func() int64,
	releaseYearMax int,
) (application.ExecutionReviewBatchResult, error) {
	var result application.ExecutionReviewBatchResult
	err := dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := executionRecords{executor: executor}
		before, found, err := records.Current(ctx, unit.JobID)
		if err != nil {
			return fmt.Errorf("read EmulationStation execution: %w", err)
		}
		if !found || before.Execution != unit {
			return application.ErrVersionConflict
		}
		if before.Kind != "SERVER_EMULATIONSTATION_IMPORT" {
			result.Before = before
			return nil
		}
		reviews, err := records.Reviews(ctx, before.ImportID, 101)
		if err != nil {
			return fmt.Errorf("read EmulationStation execution reviews: %w", err)
		}
		for _, review := range reviews[:min(len(reviews), 100)] {
			preparation, err := application.ReviewPreparation(review)
			if err != nil {
				return err
			}
			now := nowFunc()
			if err := records.Fence(ctx, before, now); err != nil {
				return fmt.Errorf("fence EmulationStation review: %w", err)
			}
			var metadata librarymodel.ServerMetadata
			if err := json.Unmarshal([]byte(review.MetadataJSON), &metadata); err != nil {
				return fmt.Errorf("decode EmulationStation review metadata: %w", err)
			}
			auditID, _ := uuid.NewV7()
			label := "release-setup"
			input := librarymodel.MetadataSeedInput{
				ItemID: review.ReservedItemID, Metadata: metadata,
				MaximumYear: releaseYearMax, NowMS: now,
				AuditID: auditID.String(), ActorKind: "SYSTEM", ActorLabel: &label,
			}
			_, additions, err := libraryrepo.SeedMetadata(ctx, executor, input)
			if err != nil {
				return fmt.Errorf("seed EmulationStation review metadata: %w", err)
			}
			warningsJSON, err := appendExecutionWarningsValue(review.WarningsJSON, additions)
			if err != nil {
				return err
			}
			err = records.CompleteReview(ctx, application.ExecutionReviewCompletion{
				Before: before, Review: review, WarningsJSON: warningsJSON,
				Preparation: preparation, NowMS: now,
			})
			if err != nil {
				return fmt.Errorf("complete EmulationStation review: %w", err)
			}
			before, found, err = records.Current(ctx, unit.JobID)
			if err != nil {
				return fmt.Errorf("re-read EmulationStation execution: %w", err)
			}
			if !found || before.Execution != unit {
				return application.ErrVersionConflict
			}
		}
		result.Before = before
		result.More = len(reviews) > 100
		return nil
	})
	if err != nil {
		return application.ExecutionReviewBatchResult{}, fmt.Errorf("commit EmulationStation review batch: %w", err)
	}
	return result, nil
}

func (repository *ExecutionControl) CommitExecutionFinish(
	ctx context.Context,
	change application.ExecutionFinish,
) error {
	err := dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := executionRecords{executor: executor}
		if err := records.fence(ctx, change); err != nil {
			return fmt.Errorf("fence EmulationStation execution finish: %w", err)
		}
		if err := records.Finish(ctx, change); err != nil {
			return fmt.Errorf("persist EmulationStation execution finish: %w", err)
		}
		if change.SchedulePayload {
			if err := scheduleTerminalPayloads(ctx, executor, change.Before.ImportID, change.NowMS); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit EmulationStation execution finish: %w", err)
	}
	return nil
}
