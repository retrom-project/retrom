package pegasusimport

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/pegasusimport"
)

type ItemWork struct {
	repository model.ItemWorkRepository
	now        func() time.Time
}

func NewItemWork(repository model.ItemWorkRepository, now func() time.Time) *ItemWork {
	return &ItemWork{repository: repository, now: now}
}

func (service *ItemWork) Next(
	ctx context.Context,
	identity model.ExecutionIdentity,
) (model.ExecutionItem, bool, error) {
	nowMS := service.now().UnixMilli()
	result, err := service.repository.ClaimNextItem(ctx, identity, nowMS)
	if err != nil {
		return model.ExecutionItem{}, false, fmt.Errorf("begin Pegasus item work: %w", err)
	}
	return result.Item, result.Found, nil
}

func (service *ItemWork) Resume(
	ctx context.Context,
	identity model.ExecutionIdentity,
	itemID, jobID, libraryItemID string,
) error {
	nowMS := service.now().UnixMilli()
	if err := service.repository.CommitItemResume(ctx, identity, itemID, jobID, libraryItemID, nowMS); err != nil {
		return fmt.Errorf("resume Pegasus item: %w", err)
	}
	return nil
}

func (service *ItemWork) Finish(
	ctx context.Context,
	identity model.ExecutionIdentity,
	itemID string,
	outcome model.ItemOutcome,
) error {
	if !model.ValidItemOutcome(outcome) {
		return model.ErrInvalid
	}
	nowMS := service.now().UnixMilli()
	if err := service.repository.CommitItemFinish(ctx, identity, itemID, outcome, nowMS); err != nil {
		return fmt.Errorf("finish Pegasus item: %w", err)
	}
	return nil
}
