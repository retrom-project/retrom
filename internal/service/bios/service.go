package bios

import (
	"context"
	"fmt"

	model "retrom/internal/model/bios"
)

type Service struct {
	repository model.Repository
}

func New(repository model.Repository) *Service {
	return &Service{repository: repository}
}

func (service *Service) List(ctx context.Context, request model.ListRequest) (model.ListResult, error) {
	if service.repository == nil || !validListRequest(request) {
		return model.ListResult{}, model.ErrInvalid
	}

	fetch := request
	fetch.Limit++
	result, err := service.repository.List(ctx, fetch)
	if err != nil {
		return model.ListResult{}, fmt.Errorf("list BIOS catalog: %w", err)
	}
	if len(result.Items) <= request.Limit {
		return result, nil
	}

	last := result.Items[request.Limit-1]
	result.Items = result.Items[:request.Limit]
	result.NextCursor = &model.Cursor{
		SortValues: []string{last.CoreName, last.LogicalName},
		ID:         last.ID,
	}
	return result, nil
}

func validListRequest(request model.ListRequest) bool {
	if request.Scope != model.ScopeRequiredByLibrary && request.Scope != model.ScopeFullCatalog {
		return false
	}
	if request.Quick != model.QuickAll && request.Quick != model.QuickAttention &&
		request.Quick != model.QuickRequired && request.Quick != model.QuickOptional {
		return false
	}
	if request.Status != "" && !validStatus(request.Status) {
		return false
	}
	return request.Limit >= 1 && request.Limit <= 100 && validCursor(request.Cursor)
}

func validStatus(status string) bool {
	switch status {
	case "MATCHED", "MISSING", "HASH_WARNING", "MISSING_ENTRY", "OPTIONAL_MISSING", "INVALID":
		return true
	default:
		return false
	}
}

func validCursor(cursor *model.Cursor) bool {
	return cursor == nil || (len(cursor.SortValues) == 2 && cursor.ID != "")
}
