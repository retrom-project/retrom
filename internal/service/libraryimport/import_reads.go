package libraryimport

import (
	"context"
	"fmt"
	model "retrom/internal/model/libraryimport"
)

type ImportReads struct {
	repository model.ImportReadRepository
}

func NewImportReads(repository model.ImportReadRepository) *ImportReads {
	return &ImportReads{repository: repository}
}

func (service *ImportReads) Summary(ctx context.Context) (model.ImportOverviewSummary, error) {
	result, err := service.repository.Summary(ctx)
	if err != nil {
		return model.ImportOverviewSummary{}, fmt.Errorf("read import overview: %w", err)
	}
	return result, nil
}

func (service *ImportReads) List(ctx context.Context, query model.ImportListQuery) ([]model.ImportListItem, error) {
	if query.Limit < 1 || query.Limit > 21 || query.SortCode == "" {
		return nil, model.ErrImportReadQuery
	}
	result, err := service.repository.List(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list imports: %w", err)
	}
	return result, nil
}

func (service *ImportReads) Detail(ctx context.Context, importJobID string) (model.ImportDetail, error) {
	result, err := service.repository.Detail(ctx, importJobID)
	if err != nil {
		return model.ImportDetail{}, fmt.Errorf("read import detail: %w", err)
	}
	return result, nil
}

func (service *ImportReads) MultiDiscItemSummaries(
	ctx context.Context,
	importJobID string,
) ([]model.ImportMultiDiscItemSummary, error) {
	result, err := service.repository.MultiDiscItemSummaries(ctx, importJobID)
	if err != nil {
		return nil, fmt.Errorf("read multi-disc item summaries: %w", err)
	}
	return result, nil
}

func (service *ImportReads) ReviewHistory(
	ctx context.Context,
	query model.ReviewHistoryQuery,
) ([]model.ReviewHistoryItem, error) {
	result, err := service.repository.ReviewHistory(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("read review history: %w", err)
	}
	return result, nil
}

func (service *ImportReads) ReviewHistoryEvent(
	ctx context.Context,
	eventID string,
) (model.ReviewHistoryEvent, error) {
	result, err := service.repository.ReviewHistoryEvent(ctx, eventID)
	if err != nil {
		return model.ReviewHistoryEvent{}, fmt.Errorf("read review history event: %w", err)
	}
	return result, nil
}
