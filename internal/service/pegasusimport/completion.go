package pegasusimport

import (
	"context"
	"fmt"
	payload "retrom/internal/service/payloadrelease"
	"time"
)

type (
	CompletionCounts struct {
		Blocked, Failed, ReviewPending, Published, ReviewDiscarded, Existing, Cancelled, Unfinished int64
	}
	CompletionChange struct {
		Before      ExecutionSnapshot
		Counts      CompletionCounts
		ImportState string
		Retryable   bool
		NowMS       int64
	}
	CompletionRecords interface {
		Payload() payload.ReleaseScope
		Current(context.Context, string) (ExecutionSnapshot, error)
		Counts(context.Context, string) (CompletionCounts, error)
		Complete(context.Context, CompletionChange) error
	}
	CompletionRepository interface {
		WithCompletion(context.Context, func(CompletionRecords) error) error
	}
	Completion struct {
		repository CompletionRepository
		now        func() time.Time
	}
)

func NewCompletion(repository CompletionRepository, now func() time.Time) *Completion {
	return &Completion{repository: repository, now: now}
}

func (service *Completion) Finish(ctx context.Context, identity ExecutionIdentity) error {
	err := service.repository.WithCompletion(ctx, func(records CompletionRecords) error {
		before, err := records.Current(ctx, identity.JobID)
		if err != nil {
			return fmt.Errorf("read Pegasus completion ownership: %w", err)
		}
		now := service.now().UnixMilli()
		if err := ValidateExecution(before, identity, now); err != nil {
			return err
		}
		if before.Kind != "SERVER_PEGASUS_IMPORT" || before.JobState != "RUNNING" {
			return ErrVersionConflict
		}
		counts, err := records.Counts(ctx, before.ImportID)
		if err != nil {
			return fmt.Errorf("read Pegasus final counts: %w", err)
		}
		if counts.Unfinished != 0 {
			return ErrVersionConflict
		}
		change := CompletionChange{
			Before:      before,
			Counts:      counts,
			ImportState: "COMPLETED",
			Retryable:   counts.Failed > 0,
			NowMS:       now,
		}
		if counts.Blocked > 0 || counts.Failed > 0 {
			change.ImportState = "PARTIAL_FAILURE"
		}
		if err := records.Complete(ctx, change); err != nil {
			return err
		}
		return scheduleTerminalPayloads(ctx, records.Payload(), before.ImportID, change.NowMS)
	})
	if err != nil {
		return fmt.Errorf("complete Pegasus execution: %w", err)
	}
	return nil
}
