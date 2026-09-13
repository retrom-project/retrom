package gamelist

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

func (service *Service) Detail(ctx context.Context, profileID, gameID string) (Detail, error) {
	if service.repository == nil || profileID == "" || gameID == "" {
		return Detail{}, ErrInvalid
	}
	result, err := service.repository.Detail(ctx, profileID, gameID)
	if err != nil {
		return Detail{}, fmt.Errorf("read game detail: %w", err)
	}
	return result, nil
}

func (service *Service) List(ctx context.Context, request ListRequest) (ListResult, error) {
	if service.repository == nil || request.ProfileID == "" || request.Limit < 1 || !validSort(request.Sort) {
		return ListResult{}, ErrInvalid
	}
	fetch := request
	fetch.Limit++
	result, err := service.repository.List(ctx, fetch)
	if err != nil {
		return ListResult{}, fmt.Errorf("list games: %w", err)
	}
	if len(result.Items) <= request.Limit {
		return result, nil
	}
	last := result.Items[request.Limit-1]
	result.Items = result.Items[:request.Limit]
	result.NextCursor = &Cursor{
		SortValues: cursorSortValues(last, request.Sort),
		ID:         last.ID,
	}
	return result, nil
}

func validSort(sort string) bool {
	switch sort {
	case SortTitleAsc, SortAddedDesc, SortRecentDesc, SortUpdatedDesc:
		return true
	default:
		return false
	}
}

func cursorSortValues(item GameItem, sort string) []string {
	switch sort {
	case SortRecentDesc:
		lastPlayed := int64(-1)
		if item.LastPlayedAtMS != nil {
			lastPlayed = *item.LastPlayedAtMS
		}
		return []string{
			formatInt64(lastPlayed), formatInt64(item.CreatedAtMS), item.Title,
		}
	case SortAddedDesc:
		return []string{formatInt64(item.CreatedAtMS), item.Title}
	case SortUpdatedDesc:
		return []string{formatInt64(item.UpdatedAtMS), item.Title}
	default:
		return []string{item.Title}
	}
}

func formatInt64(value int64) string {
	return fmt.Sprintf("%d", value)
}
