package bios

import (
	"context"
	"fmt"
)

type Service struct {
	repository Repository
}

func New(repository Repository) *Service {
	return &Service{repository: repository}
}

func (service *Service) List(ctx context.Context, request ListRequest) (ListResult, error) {
	if service.repository == nil || !validListRequest(request) {
		return ListResult{}, ErrInvalid
	}

	fetch := request
	fetch.Limit++
	result, err := service.repository.List(ctx, fetch)
	if err != nil {
		return ListResult{}, fmt.Errorf("list BIOS catalog: %w", err)
	}
	if len(result.Items) <= request.Limit {
		return result, nil
	}

	last := result.Items[request.Limit-1]
	result.Items = result.Items[:request.Limit]
	result.NextCursor = &Cursor{
		SortValues: []string{last.CoreName, last.LogicalName},
		ID:         last.ID,
	}
	return result, nil
}

func validListRequest(request ListRequest) bool {
	if request.Scope != ScopeRequiredByLibrary && request.Scope != ScopeFullCatalog {
		return false
	}
	if request.Quick != QuickAll && request.Quick != QuickAttention &&
		request.Quick != QuickRequired && request.Quick != QuickOptional {
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

func validCursor(cursor *Cursor) bool {
	return cursor == nil || (len(cursor.SortValues) == 2 && cursor.ID != "")
}
