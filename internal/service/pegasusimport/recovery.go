package pegasusimport

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	library "retrom/internal/service/libraryimport"
)

type (
	RecoverySnapshot struct {
		JobID, ImportID, Kind, JobState, ImportState, WorkerID       string
		JobVersion, ImportVersion, ExecutionNo, Attempt, MaxAttempts int64
		LeaseUntilMS, DeadlineMS                                     int64
	}
	RecoveryChange struct {
		Before                                                  RecoverySnapshot
		JobState, ImportState, ItemState, Code, ItemCode, Event string
		NowMS                                                   int64
	}
	RecoveryReviewChange struct {
		Execution RecoverySnapshot
		Handoff   ReviewHandoffChange
	}
	RecoveryRecords interface {
		Current(context.Context, string) (RecoverySnapshot, error)
		Reviews(context.Context, string, int) ([]ReviewHandoffSnapshot, error)
		CompleteReview(context.Context, RecoveryReviewChange) error
		Apply(context.Context, RecoveryChange) error
	}
	RecoveryScope struct {
		Records  RecoveryRecords
		Metadata library.MetadataScope
	}
	RecoveryRepository interface {
		ExpiredExecutions(context.Context, int64, int) ([]RecoverySnapshot, error)
		WithRecovery(context.Context, func(RecoveryScope) error) error
	}
	Recovery struct {
		repository RecoveryRepository
		metadata   ReviewMetadataSeeder
		now        func() time.Time
	}
)

func NewRecovery(repository RecoveryRepository, metadata ReviewMetadataSeeder, now func() time.Time) *Recovery {
	return &Recovery{repository: repository, metadata: metadata, now: now}
}

func (service *Recovery) Recover(ctx context.Context) error {
	candidates, err := service.repository.ExpiredExecutions(ctx, service.now().UnixMilli(), 100)
	if err != nil {
		return fmt.Errorf("list expired Pegasus executions: %w", err)
	}
	for _, candidate := range candidates {
		err := service.repository.WithRecovery(ctx, func(scope RecoveryScope) error {
			return service.recoverExecution(ctx, scope, candidate)
		})
		if errors.Is(err, ErrVersionConflict) {
			continue
		}
		if err != nil {
			return fmt.Errorf("recover Pegasus execution %s: %w", candidate.JobID, err)
		}
	}
	return nil
}

func (service *Recovery) recoverExecution(ctx context.Context, scope RecoveryScope, candidate RecoverySnapshot) error {
	before, err := scope.Records.Current(ctx, candidate.JobID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read Pegasus recovery candidate: %w", err)
	}
	if before != candidate {
		return ErrVersionConflict
	}
	now := service.now()
	if _, err := planRecovery(before, now.UnixMilli()); err != nil {
		return err
	}
	reviews, err := scope.Records.Reviews(ctx, before.ImportID, 101)
	if err != nil {
		return fmt.Errorf("read interrupted Pegasus reviews: %w", err)
	}
	for _, review := range reviews[:min(len(reviews), 100)] {
		// Every completed review advances the parent version in this transaction.
		current, err := currentRecovery(ctx, scope.Records, before)
		if err != nil {
			return err
		}
		review.ImportVersion = current.ImportVersion
		if err := service.recoverReview(ctx, scope, current, review, now); err != nil {
			return err
		}
	}
	if len(reviews) > 100 {
		return nil
	}
	current, err := currentRecovery(ctx, scope.Records, before)
	if err != nil {
		return err
	}
	change, err := planRecovery(current, now.UnixMilli())
	if err != nil {
		return err
	}
	if err := scope.Records.Apply(ctx, change); err != nil {
		return fmt.Errorf("persist Pegasus recovery: %w", err)
	}
	return nil
}

func (service *Recovery) recoverReview(
	ctx context.Context, scope RecoveryScope, execution RecoverySnapshot, review ReviewHandoffSnapshot, now time.Time,
) error {
	if review.Identity.JobID != execution.JobID || review.Identity.ImportID != execution.ImportID ||
		review.Identity.ExecutionNo != execution.ExecutionNo || review.Identity.Attempt != execution.Attempt ||
		review.Identity.LibraryItemID == "" || review.Identity.LibraryJobID == "" ||
		review.Version < 1 || review.Version == math.MaxInt64 {
		return ErrVersionConflict
	}
	if review.State != "PENDING" && review.State != "COPYING" && review.State != "VALIDATING" {
		return ErrVersionConflict
	}
	_, warnings, err := service.metadata.SeedInScope(
		ctx,
		scope.Metadata,
		review.Identity.LibraryItemID,
		review.Metadata,
		now.UTC().Year()+1,
	)
	if err != nil {
		return fmt.Errorf("seed recovered Pegasus review: %w", err)
	}
	err = scope.Records.CompleteReview(ctx, RecoveryReviewChange{Execution: execution, Handoff: ReviewHandoffChange{
		Before: review, Warnings: mergeReviewMetadataWarnings(review.Warnings, warnings), NowMS: now.UnixMilli(),
	}})
	if err != nil {
		return fmt.Errorf("complete recovered Pegasus review: %w", err)
	}
	return nil
}

func planRecovery(before RecoverySnapshot, now int64) (RecoveryChange, error) {
	if before.LeaseUntilMS > now {
		return RecoveryChange{}, ErrVersionConflict
	}
	if !validRecoveryExecution(before) {
		return RecoveryChange{}, fmt.Errorf("invalid expired Pegasus execution: %w", ErrInvalid)
	}
	change := RecoveryChange{
		Before: before, JobState: "QUEUED", ImportState: "QUEUED",
		ItemState: "PENDING", Event: "RETRY_SCHEDULED", NowMS: now,
	}
	if before.Kind == "SERVER_PEGASUS_SCAN" {
		change.ImportState = "SCANNING"
	} else if before.Kind != "SERVER_PEGASUS_IMPORT" {
		return RecoveryChange{}, ErrInvalid
	}
	if before.JobState == "CANCEL_REQUESTED" && before.ImportState == "CANCEL_REQUESTED" {
		change.JobState, change.ImportState = "CANCELLED", "CANCELLED"
		change.ItemState, change.Event, change.ItemCode = "CANCELLED", "CANCELLED", "CANCELLED"
		return change, nil
	}
	if before.JobState != "RUNNING" || !recoveryParentActive(before) {
		return RecoveryChange{}, ErrVersionConflict
	}
	if before.DeadlineMS <= now {
		change.Code = "PEGASUS_EXECUTION_TIMEOUT"
	} else if before.Attempt >= before.MaxAttempts {
		change.Code = "PEGASUS_WORKER_ATTEMPTS_EXHAUSTED"
	}
	if change.Code != "" {
		change.ItemCode = change.Code
		change.JobState, change.ImportState, change.ItemState, change.Event = "FAILED", "FAILED", "COMMIT_FAILED", "FAILED"
	}
	return change, nil
}

func recoveryParentActive(before RecoverySnapshot) bool {
	if before.Kind == "SERVER_PEGASUS_SCAN" {
		return before.ImportState == "SCANNING"
	}
	return before.ImportState == "RUNNING" || before.ImportState == "QUEUED"
}

func validRecoveryExecution(before RecoverySnapshot) bool {
	return before.LeaseUntilMS > 0 && before.DeadlineMS > 0 && before.Attempt > 0 && before.MaxAttempts > 0 &&
		before.ExecutionNo > 0 &&
		before.JobVersion > 0 && before.JobVersion < math.MaxInt64 && before.ImportVersion > 0 &&
		before.ImportVersion < math.MaxInt64-1
}

func currentRecovery(ctx context.Context, records RecoveryRecords, before RecoverySnapshot) (RecoverySnapshot, error) {
	current, err := records.Current(ctx, before.JobID)
	if err != nil {
		return RecoverySnapshot{}, fmt.Errorf("reread Pegasus recovery execution: %w", err)
	}
	expected := before
	expected.ImportVersion = current.ImportVersion
	if expected != current {
		return RecoverySnapshot{}, ErrVersionConflict
	}
	return current, nil
}
