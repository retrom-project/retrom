package pegasusimport

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	model "retrom/internal/model/pegasusimport"
)

type Recovery struct {
	repository model.RecoveryRepository
	now        func() time.Time
}

func NewRecovery(
	repository model.RecoveryRepository,
	_ model.ReviewMetadataSeeder,
	now func() time.Time,
) *Recovery {
	return &Recovery{repository: repository, now: now}
}

func (service *Recovery) Recover(ctx context.Context) error {
	candidates, err := service.repository.ExpiredExecutions(ctx, service.now().UnixMilli(), 100)
	if err != nil {
		return fmt.Errorf("list expired Pegasus executions: %w", err)
	}
	for _, candidate := range candidates {
		if err := service.recoverCandidate(ctx, candidate); err != nil {
			if errors.Is(err, model.ErrVersionConflict) {
				continue
			}
			return fmt.Errorf("recover Pegasus execution %s: %w", candidate.JobID, err)
		}
	}
	return nil
}

func (service *Recovery) recoverCandidate(ctx context.Context, candidate model.RecoverySnapshot) error {
	current, err := service.repository.CurrentRecovery(ctx, candidate.JobID)
	if errors.Is(err, model.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read Pegasus recovery candidate: %w", err)
	}
	if current != candidate {
		return model.ErrVersionConflict
	}
	now := service.now()
	if _, err := planRecovery(current, now.UnixMilli()); err != nil {
		return err
	}

	id := model.ExecutionIdentity{
		JobID: current.JobID, ImportID: current.ImportID,
		WorkerID: current.WorkerID, ExecutionNo: current.ExecutionNo,
		Attempt: current.Attempt,
	}

	batch, err := service.repository.CommitRecoveryReviewBatch(ctx, id, now.UnixMilli(), now.UTC().Year()+1)
	if err != nil {
		return err
	}
	current = batch.Before
	if batch.More {
		return nil
	}

	change, err := planRecovery(current, now.UnixMilli())
	if err != nil {
		return err
	}
	return service.repository.CommitRecovery(ctx, change)
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
