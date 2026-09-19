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
	nowMS := service.now().UnixMilli()
	if err := service.repository.CommitCompletion(ctx, identity, nowMS); err != nil {
		return fmt.Errorf("complete Pegasus execution: %w", err)
	}
	return nil
}
