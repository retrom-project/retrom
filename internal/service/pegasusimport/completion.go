package pegasusimport

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/pegasusimport"
)

type Completion struct {
	repository model.CompletionRepository
	now        func() time.Time
}

func NewCompletion(repository model.CompletionRepository, now func() time.Time) *Completion {
	return &Completion{repository: repository, now: now}
}

func (service *Completion) Finish(ctx context.Context, identity model.ExecutionIdentity) error {
	err := service.repository.WithCompletion(ctx, func(records model.CompletionRecords) error {
		before, err := records.Current(ctx, identity.JobID)
		if err != nil {
			return fmt.Errorf("read Pegasus completion ownership: %w", err)
		}
		now := service.now().UnixMilli()
		if err := ValidateExecution(before, identity, now); err != nil {
			return err
		}
		if before.Kind != "SERVER_PEGASUS_IMPORT" || before.JobState != "RUNNING" {
			return model.ErrVersionConflict
		}
		counts, err := records.Counts(ctx, before.ImportID)
		if err != nil {
			return fmt.Errorf("read Pegasus final counts: %w", err)
		}
		if counts.Unfinished != 0 {
			return model.ErrVersionConflict
		}
		change := model.CompletionChange{
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
			return fmt.Errorf("complete Pegasus import records: %w", err)
		}
		return scheduleTerminalPayloads(ctx, records.Payload(), before.ImportID, change.NowMS)
	})
	if err != nil {
		return fmt.Errorf("complete Pegasus execution: %w", err)
	}
	return nil
}
