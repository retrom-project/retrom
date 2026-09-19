package emulationstationimport

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type Completion struct {
	repository model.CompletionRepository
	now        func() time.Time
}

func NewCompletion(repository model.CompletionRepository, now func() time.Time) *Completion {
	return &Completion{repository: repository, now: now}
}

func (service *Completion) Finish(ctx context.Context, unit model.Execution) error {
	now := service.now().UnixMilli()
	if err := service.repository.CommitCompletion(ctx, unit, now); err != nil {
		return fmt.Errorf("complete EmulationStation import: %w", err)
	}
	return nil
}

func planCompletion(
	before model.LeaseSnapshot,
	counts model.CompletionCounts,
	now int64,
) (model.CompletionChange, error) {
	return model.PlanCompletion(before, counts, now)
}
