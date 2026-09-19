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

func (repository *Recovery) CurrentRecovery(ctx context.Context, jobID string) (application.LeaseSnapshot, bool, error) {
	conn, err := repository.database.Conn(ctx)
	if err != nil {
		return application.LeaseSnapshot{}, false, fmt.Errorf("acquire ES recovery read: %w", err)
	}
	defer conn.Close()
	return (leaseRecords{executor: conn}).Current(ctx, jobID)
}

func (repository *Recovery) CommitRecoveryReviewBatch(
	ctx context.Context,
	candidate application.LeaseSnapshot,
	nowMS int64,
	releaseYearMax int,
) (application.RecoveryReviewBatchResult, error) {
	if candidate.Kind != "SERVER_EMULATIONSTATION_IMPORT" {
		return application.RecoveryReviewBatchResult{Before: candidate, Found: true}, nil
	}
	var result application.RecoveryReviewBatchResult
	err := dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := recoveryRecords{transaction: nil, executor: executor}
		current, found, err := records.Current(ctx, candidate.JobID)
		if err != nil {
			return fmt.Errorf("read ES recovery: %w", err)
		}
		if !found {
			result.Found = false
			return nil
		}
		result.Found = true

		if current.Execution != candidate.Execution {
			return application.ErrVersionConflict
		}

		reviews, err := records.Reviews(ctx, current.ImportID, 101)
		if err != nil {
			return fmt.Errorf("read ES recovery reviews: %w", err)
		}
		for _, review := range reviews[:min(len(reviews), 100)] {
			preparation, err := application.ReviewPreparation(review)
			if err != nil {
				return err
			}
			if err := records.Fence(ctx, current, nowMS); err != nil {
				return fmt.Errorf("fence ES recovery review: %w", err)
			}

			var metadata librarymodel.ServerMetadata
			if err := json.Unmarshal([]byte(review.MetadataJSON), &metadata); err != nil {
				return fmt.Errorf("decode ES recovery metadata: %w", err)
			}
			auditID, _ := uuid.NewV7()
			label := "release-setup"
			input := librarymodel.MetadataSeedInput{
				ItemID: review.ReservedItemID, Metadata: metadata,
				MaximumYear: releaseYearMax, NowMS: nowMS,
				AuditID: auditID.String(), ActorKind: "SYSTEM", ActorLabel: &label,
			}
			_, additions, err := libraryrepo.SeedMetadata(ctx, executor, input)
			if err != nil {
				return fmt.Errorf("seed ES recovery review metadata: %w", err)
			}
			warnings, err := appendRecoveryWarnings(review.WarningsJSON, additions)
			if err != nil {
				return err
			}
			err = records.CompleteReview(ctx, application.ExecutionReviewCompletion{
				Before: current, Review: review, WarningsJSON: warnings, Preparation: preparation, NowMS: nowMS,
			})
			if err != nil {
				return fmt.Errorf("complete ES recovery review: %w", err)
			}
			current, _, err = records.Current(ctx, candidate.JobID)
			if err != nil {
				return fmt.Errorf("re-read ES recovery: %w", err)
			}
		}
		result.Before = current
		result.More = len(reviews) > 100
		return nil
	})
	if err != nil {
		return application.RecoveryReviewBatchResult{}, fmt.Errorf("commit ES recovery review batch: %w", err)
	}
	return result, nil
}

func (repository *Recovery) CommitRecovery(ctx context.Context, change application.RecoveryChange) error {
	err := dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := recoveryRecords{transaction: nil, executor: executor}
		if err := records.Apply(ctx, change); err != nil {
			return fmt.Errorf("persist ES recovery: %w", err)
		}
		if change.SchedulePayload {
			return scheduleTerminalPayloads(ctx, executor, change.Before.ImportID, change.NowMS)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit ES recovery: %w", err)
	}
	return nil
}

func appendRecoveryWarnings(encoded string, additions []librarymodel.ServerMetadataWarning) (string, error) {
	var warnings []map[string]any
	if err := json.Unmarshal([]byte(encoded), &warnings); err != nil {
		return "", fmt.Errorf("decode ES warnings: %w", err)
	}
	if warnings == nil {
		return "", application.ErrInvalid
	}
	for _, addition := range additions {
		found := false
		for _, existing := range warnings {
			if existing["code"] == addition.Code && existing["field"] == addition.Field {
				found = true
				break
			}
		}
		if !found {
			warnings = append(warnings, map[string]any{"code": addition.Code, "field": addition.Field})
		}
	}
	warnings = application.BoundedWarnings(warnings)
	value, err := json.Marshal(warnings)
	if err != nil {
		return "", fmt.Errorf("encode ES warnings: %w", err)
	}
	return string(value), nil
}
