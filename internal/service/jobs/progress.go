package jobs

import (
	"context"
	"fmt"

	model "retrom/internal/model/jobs"
)

func (service *Service) Get(ctx context.Context, id string) (model.Snapshot, error) {
	snapshot, err := service.repository.LoadDetail(ctx, id)
	if err != nil {
		return model.Snapshot{}, fmt.Errorf("read job detail: %w", err)
	}
	return snapshot, nil
}

func (service *Service) JobStreamSnapshot(ctx context.Context, id string) (model.Snapshot, int64, error) {
	snapshot, maximum, err := service.repository.LoadJobStreamSnapshot(ctx, id)
	if err != nil {
		return model.Snapshot{}, 0, fmt.Errorf("read stream snapshot: %w", err)
	}
	return snapshot, maximum, nil
}

func (service *Service) ImportStreamSnapshot(ctx context.Context, id string) (model.ImportProgress, int64, error) {
	snapshot, maximum, err := service.repository.LoadImportStreamSnapshot(ctx, id)
	if err != nil {
		return model.ImportProgress{}, 0, fmt.Errorf("read stream snapshot: %w", err)
	}
	return snapshot, maximum, nil
}

func (service *Service) JobEvents(ctx context.Context, id string, after int64) (model.EventBatch, error) {
	events, snapshot, err := service.repository.LoadJobEvents(ctx, id, after)
	if err != nil {
		return model.EventBatch{}, fmt.Errorf("read event batch snapshot: %w", err)
	}
	return progressBatch(events, jobTerminal(snapshot.State)), nil
}

func (service *Service) ImportEvents(ctx context.Context, id string, after int64) (model.EventBatch, error) {
	events, snapshot, err := service.repository.LoadImportEvents(ctx, id, after)
	if err != nil {
		return model.EventBatch{}, fmt.Errorf("read event batch snapshot: %w", err)
	}
	return progressBatch(events, importTerminal(snapshot.State)), nil
}

func progressBatch(events []model.Event, terminal bool) model.EventBatch {
	return model.EventBatch{Events: events, Terminal: terminal && len(events) < model.EventBatchSize}
}

func jobTerminal(state string) bool {
	return state == "SUCCEEDED" || state == "FAILED" || state == "CANCELLED"
}

func importTerminal(state string) bool {
	return state == "COMPLETED" || state == "FAILED" || state == "CANCELLED" ||
		state == "REVIEW_PENDING" || state == "PARTIAL_FAILURE"
}
