package pegasusimport

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/pegasusimport"
)

type WorkerSettlement struct {
	repository model.WorkerSettlementRepository
	now        func() time.Time
}

func NewWorkerSettlement(
	repository model.WorkerSettlementRepository,
	_ model.ReviewMetadataSeeder,
	now func() time.Time,
) *WorkerSettlement {
	return &WorkerSettlement{repository: repository, now: now}
}

func (service *WorkerSettlement) Fail(
	ctx context.Context,
	id model.ExecutionIdentity,
	failure model.ExecutionFailure,
) error {
	if failure.Code == "" {
		return model.ErrInvalid
	}
	_, err := service.settle(ctx, id, &failure)
	return err
}

func (service *WorkerSettlement) Cancelled(ctx context.Context, id model.ExecutionIdentity) (bool, error) {
	return service.settle(ctx, id, nil)
}

func (service *WorkerSettlement) settle(
	ctx context.Context,
	id model.ExecutionIdentity,
	failure *model.ExecutionFailure,
) (bool, error) {
	for {
		before, err := service.repository.CurrentSettlement(ctx, id.JobID)
		if err != nil {
			return false, fmt.Errorf("read Pegasus settlement owner: %w", err)
		}
		if err := ValidateExecution(before, id, service.now().UnixMilli()); err != nil {
			return false, err
		}
		if before.Kind != "SERVER_PEGASUS_IMPORT" && before.Kind != "SERVER_PEGASUS_SCAN" {
			return false, model.ErrInvalid
		}

		cancel := before.JobState == "CANCEL_REQUESTED"
		if failure == nil && !cancel {
			return false, nil
		}

		releaseYearMax := service.now().UTC().Year() + 1
		batch, err := service.repository.CommitSettlementReviewBatch(ctx, id, func() int64 { return service.now().UnixMilli() }, releaseYearMax)
		if err != nil {
			return false, fmt.Errorf("settle Pegasus worker: %w", err)
		}
		if batch.More {
			continue
		}

		current := batch.Before
		if err := ValidateExecution(current, id, service.now().UnixMilli()); err != nil {
			return false, err
		}
		change := model.WorkerSettlementChange{Before: current, State: "CANCELLED", NowMS: service.now().UnixMilli()}
		if !cancel {
			change.State = "FAILED"
			change.Failure = *failure
		}
		if err := service.repository.CommitSettlement(ctx, change); err != nil {
			return false, fmt.Errorf("settle Pegasus worker: %w", err)
		}
		return true, nil
	}
}
