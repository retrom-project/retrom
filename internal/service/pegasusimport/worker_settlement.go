package pegasusimport

import (
	"context"
	"fmt"
	"time"

	library "retrom/internal/service/libraryimport"
)

type (
	ExecutionFailure struct {
		Code      string
		Retryable bool
	}
	WorkerSettlementChange struct {
		Before  ExecutionSnapshot
		State   string
		Failure ExecutionFailure
		NowMS   int64
	}
	WorkerSettlementReader interface {
		Current(context.Context, string) (ExecutionSnapshot, error)
		Reviews(context.Context, string, int) ([]ReviewHandoffSnapshot, error)
	}
	WorkerSettlementWriter interface {
		CompleteReview(context.Context, RecoveryReviewChange) error
		Close(context.Context, WorkerSettlementChange) error
	}
	WorkerSettlementScope struct {
		Read     WorkerSettlementReader
		Write    WorkerSettlementWriter
		Metadata library.MetadataScope
	}
	WorkerSettlementRepository interface {
		WithSettlement(context.Context, func(WorkerSettlementScope) error) error
	}
	WorkerSettlement struct {
		repository WorkerSettlementRepository
		metadata   ReviewMetadataSeeder
		now        func() time.Time
	}
)

func NewWorkerSettlement(
	repository WorkerSettlementRepository,
	metadata ReviewMetadataSeeder,
	now func() time.Time,
) *WorkerSettlement {
	return &WorkerSettlement{repository: repository, metadata: metadata, now: now}
}

func (service *WorkerSettlement) Fail(ctx context.Context, id ExecutionIdentity, failure ExecutionFailure) error {
	if failure.Code == "" {
		return ErrInvalid
	}
	_, err := service.settle(ctx, id, &failure)
	return err
}

func (service *WorkerSettlement) Cancelled(ctx context.Context, id ExecutionIdentity) (bool, error) {
	return service.settle(ctx, id, nil)
}

func (service *WorkerSettlement) settle(
	ctx context.Context,
	id ExecutionIdentity,
	failure *ExecutionFailure,
) (bool, error) {
	for {
		closed, more := false, false
		err := service.repository.WithSettlement(ctx, func(scope WorkerSettlementScope) error {
			var err error
			closed, more, err = service.settleInScope(ctx, scope, id, failure)
			return err
		})
		if err != nil {
			return false, fmt.Errorf("settle Pegasus worker: %w", err)
		}
		if !more {
			return closed, nil
		}
	}
}

func (service *WorkerSettlement) current(
	ctx context.Context,
	read WorkerSettlementReader,
	id ExecutionIdentity,
) (ExecutionSnapshot, error) {
	before, err := read.Current(ctx, id.JobID)
	if err != nil {
		return ExecutionSnapshot{}, fmt.Errorf("read Pegasus settlement owner: %w", err)
	}
	if err := ValidateExecution(before, id, service.now().UnixMilli()); err != nil {
		return ExecutionSnapshot{}, err
	}
	if before.Kind != "SERVER_PEGASUS_IMPORT" && before.Kind != "SERVER_PEGASUS_SCAN" {
		return ExecutionSnapshot{}, ErrInvalid
	}
	return before, nil
}

func (service *WorkerSettlement) reconcile(
	ctx context.Context,
	scope WorkerSettlementScope,
	id ExecutionIdentity,
	before ExecutionSnapshot,
) (bool, error) {
	if before.Kind == "SERVER_PEGASUS_SCAN" {
		return false, nil
	}
	reviews, err := scope.Read.Reviews(ctx, before.ImportID, 101)
	if err != nil {
		return false, fmt.Errorf("read Pegasus settlement reviews: %w", err)
	}
	for _, review := range reviews[:min(len(reviews), 100)] {
		current, err := service.current(ctx, scope.Read, id)
		if err != nil {
			return false, err
		}
		review.ImportVersion = current.ImportVersion
		change, err := prepareRecoveryReview(ctx, service.metadata, scope.Metadata, current, review, service.now())
		if err != nil {
			return false, err
		}
		if err := scope.Write.CompleteReview(ctx, change); err != nil {
			return false, fmt.Errorf("retain Pegasus review on settlement: %w", err)
		}
	}
	return len(reviews) > 100, nil
}

func (service *WorkerSettlement) settleInScope(
	ctx context.Context,
	scope WorkerSettlementScope,
	id ExecutionIdentity,
	failure *ExecutionFailure,
) (bool, bool, error) {
	before, err := service.current(ctx, scope.Read, id)
	if err != nil {
		return false, false, err
	}
	cancel := before.JobState == "CANCEL_REQUESTED"
	if failure == nil && !cancel {
		return false, false, nil
	}
	more, err := service.reconcile(ctx, scope, id, before)
	if err != nil || more {
		return false, more, err
	}
	current, err := service.current(ctx, scope.Read, id)
	if err != nil {
		return false, false, err
	}
	change := WorkerSettlementChange{Before: current, State: "CANCELLED", NowMS: service.now().UnixMilli()}
	if !cancel {
		change.State = "FAILED"
		change.Failure = *failure
	}
	if err := ValidateExecution(current, id, change.NowMS); err != nil {
		return false, false, err
	}
	if err := scope.Write.Close(ctx, change); err != nil {
		return false, false, fmt.Errorf("persist Pegasus worker settlement: %w", err)
	}
	return true, false, nil
}
