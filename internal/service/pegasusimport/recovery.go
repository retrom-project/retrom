package pegasusimport

import (
	"context"
	"errors"
	"fmt"
	"math"
	model "retrom/internal/model/pegasusimport"
	"time"
)

type Recovery struct {
	repository model.RecoveryRepository
	metadata   model.ReviewMetadataSeeder
	now        func() time.Time
}

func NewRecovery(repository model.RecoveryRepository, metadata model.ReviewMetadataSeeder, now func() time.Time) *Recovery {
	return &Recovery{repository: repository, metadata: metadata, now: now}
}

func (service *Recovery) Recover(ctx context.Context) error {
	candidates, err := service.repository.ExpiredExecutions(ctx, service.now().UnixMilli(), 100)
	if err != nil {
		return fmt.Errorf("list expired Pegasus executions: %w", err)
	}
	for _, candidate := range candidates {
		err := service.repository.WithRecovery(ctx, func(scope model.RecoveryScope) error {
			return service.recoverExecution(ctx, scope, candidate)
		})
		if errors.Is(err, model.ErrVersionConflict) {
			continue
		}
		if err != nil {
			return fmt.Errorf("recover Pegasus execution %s: %w", candidate.JobID, err)
		}
	}
	return nil
}

func (service *Recovery) recoverExecution(ctx context.Context, scope model.RecoveryScope, candidate model.RecoverySnapshot) error {
	before, err := scope.Records.Current(ctx, candidate.JobID)
	if errors.Is(err, model.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read Pegasus recovery candidate: %w", err)
	}
	if before != candidate {
		return model.ErrVersionConflict
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
	if change.JobState != "QUEUED" {
		return scheduleTerminalPayloads(ctx, scope.Payload, current.ImportID, change.NowMS)
	}
	return nil
}

func (service *Recovery) recoverReview(
	ctx context.Context, scope model.RecoveryScope, execution model.RecoverySnapshot, review model.ReviewHandoffSnapshot, now time.Time,
) error {
	change, err := prepareRecoveryReview(ctx, service.metadata, scope.Metadata, execution, review, now)
	if err != nil {
		return err
	}
	err = scope.Records.CompleteReview(ctx, change)
	if err != nil {
		return fmt.Errorf("complete recovered Pegasus review: %w", err)
	}
	return nil
}

func planRecovery(before model.RecoverySnapshot, now int64) (model.RecoveryChange, error) {
	if before.LeaseUntilMS > now {
		return model.RecoveryChange{}, model.ErrVersionConflict
	}
	if !validRecoveryExecution(before) {
		return model.RecoveryChange{}, fmt.Errorf("invalid expired Pegasus execution: %w", model.ErrInvalid)
	}
	change := model.RecoveryChange{
		Before: before, JobState: "QUEUED", ImportState: "QUEUED",
		ItemState: "PENDING", Event: "RETRY_SCHEDULED", NowMS: now,
	}
	if before.Kind == "SERVER_PEGASUS_SCAN" {
		change.ImportState = "SCANNING"
	} else if before.Kind != "SERVER_PEGASUS_IMPORT" {
		return model.RecoveryChange{}, model.ErrInvalid
	}
	if before.JobState == "CANCEL_REQUESTED" && before.ImportState == "CANCEL_REQUESTED" {
		change.JobState, change.ImportState = "CANCELLED", "CANCELLED"
		change.ItemState, change.Event, change.ItemCode = "CANCELLED", "CANCELLED", "CANCELLED"
		return change, nil
	}
	if (before.JobState != "RUNNING" && before.JobState != "QUEUED") || !recoveryParentActive(before) {
		return model.RecoveryChange{}, model.ErrVersionConflict
	}
	if before.DeadlineMS <= now {
		change.Code = "PEGASUS_EXECUTION_TIMEOUT"
	} else if before.Attempt >= before.MaxAttempts {
		change.Code = "PEGASUS_WORKER_ATTEMPTS_EXHAUSTED"
	}
	if before.JobState == "QUEUED" && change.Code == "" {
		return model.RecoveryChange{}, model.ErrVersionConflict
	}
	if change.Code != "" {
		change.ItemCode = change.Code
		change.JobState, change.ImportState, change.ItemState, change.Event = "FAILED", "FAILED", "COMMIT_FAILED", "FAILED"
	}
	return change, nil
}

func recoveryParentActive(before model.RecoverySnapshot) bool {
	if before.Kind == "SERVER_PEGASUS_SCAN" {
		return before.ImportState == "SCANNING"
	}
	return before.ImportState == "RUNNING" || before.ImportState == "QUEUED"
}

func validRecoveryExecution(before model.RecoverySnapshot) bool {
	validLease := before.LeaseUntilMS > 0
	if before.JobState == "QUEUED" {
		validLease = before.LeaseUntilMS == 0 && before.WorkerID == ""
	}
	return validLease && before.DeadlineMS > 0 && before.Attempt > 0 && before.MaxAttempts > 0 &&
		before.ExecutionNo > 0 &&
		before.JobVersion > 0 && before.JobVersion < math.MaxInt64 && before.ImportVersion > 0 &&
		before.ImportVersion < math.MaxInt64-1
}

func currentRecovery(ctx context.Context, records model.RecoveryRecords, before model.RecoverySnapshot) (model.RecoverySnapshot, error) {
	current, err := records.Current(ctx, before.JobID)
	if err != nil {
		return model.RecoverySnapshot{}, fmt.Errorf("reread Pegasus recovery execution: %w", err)
	}
	expected := before
	expected.ImportVersion = current.ImportVersion
	if expected != current {
		return model.RecoverySnapshot{}, model.ErrVersionConflict
	}
	return current, nil
}
