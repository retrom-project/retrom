package emulationstationimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/emulationstationimport"
)

func (service *ItemWork) Next(ctx context.Context, unit model.Execution) (model.ExecutionItem, bool, error) {
	nowMS := service.now().UnixMilli()
	result, err := service.repository.ClaimNextItem(ctx, unit, nowMS)
	if err != nil {
		return model.ExecutionItem{}, false, fmt.Errorf("begin EmulationStation item work: %w", err)
	}
	return result.Item, result.Found, nil
}

func (service *ItemWork) Resume(ctx context.Context, unit model.Execution, itemID, jobID, ordinaryID string) error {
	nowMS := service.now().UnixMilli()
	if err := service.repository.CommitItemResume(ctx, unit, itemID, jobID, ordinaryID, nowMS); err != nil {
		return fmt.Errorf("resume EmulationStation item: %w", err)
	}
	return nil
}

func (service *ItemWork) Finish(ctx context.Context, unit model.Execution, id string, outcome model.ItemOutcome) error {
	if !validItemOutcome(outcome) {
		return model.ErrInvalid
	}
	nowMS := service.now().UnixMilli()
	if err := service.repository.CommitItemFinish(ctx, unit, id, outcome, nowMS); err != nil {
		return fmt.Errorf("finish EmulationStation item: %w", err)
	}
	return nil
}
