package tagging

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

type Service struct {
	repository Repository
	now        func() time.Time
}

func New(repository Repository, now func() time.Time) *Service {
	return &Service{repository: repository, now: now}
}

func repositoryError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("tagging: %s: %w", operation, err)
}

func (service *Service) Get(ctx context.Context, tagID string) (AdminItem, error) {
	if !ValidID(tagID) {
		return AdminItem{}, ErrNotFound
	}
	item, err := service.repository.Get(ctx, tagID)
	return item, repositoryError("get", err)
}

func (service *Service) List(ctx context.Context, filter ListFilter) ([]AdminItem, error) {
	if !validListFilter(filter) {
		return nil, ErrInvalid
	}
	query := ListQuery{
		Status: filter.Status,
		Sort:   filter.Sort,
		SearchText: canonicalSearch(
			filter.Query,
		),
		Limit:   filter.Limit,
		AfterID: filter.AfterID,
	}
	if err := applyListCursor(filter, &query); err != nil {
		return nil, err
	}

	items, err := service.repository.List(ctx, query)
	return items, repositoryError("list", err)
}

func validListFilter(filter ListFilter) bool {
	if filter.Status != StatusActive && filter.Status != StatusDeleted && filter.Status != "ALL" {
		return false
	}
	if filter.Sort != SortNameAsc && filter.Sort != SortUpdatedDesc {
		return false
	}
	return filter.Limit >= 1 && filter.Limit <= MaximumListLimit+1
}

func (service *Service) Summary(ctx context.Context) (Summary, error) {
	result, err := service.repository.Summary(ctx)
	return result, repositoryError("summary", err)
}

func (service *Service) references(ctx context.Context, kind OwnerKind, ids []string) (map[string][]Reference, error) {
	result, err := service.repository.References(ctx, kind, ids)
	return result, repositoryError("references", err)
}

func (service *Service) References(ctx context.Context, ids []string) (map[string][]Reference, error) {
	return service.references(ctx, OwnerGame, ids)
}

func (service *Service) ReviewReferences(ctx context.Context, ids []string) (map[string][]Reference, error) {
	return service.references(ctx, OwnerReviewItem, ids)
}

func (service *Service) SourceReferences(ctx context.Context, ids []string) (map[string][]Reference, error) {
	return service.references(ctx, OwnerSourceCollection, ids)
}

func applyListCursor(filter ListFilter, query *ListQuery) error {
	if filter.AfterID == "" {
		return nil
	}
	if !ValidID(filter.AfterID) || len(filter.AfterValues) != 1 {
		return ErrInvalid
	}
	if filter.Sort == SortNameAsc {
		query.AfterNameKey = filter.AfterValues[0]
		return nil
	}
	value, err := strconv.ParseInt(filter.AfterValues[0], 10, 64)
	if err != nil || value < 0 {
		return ErrInvalid
	}
	query.AfterUpdatedAt = value
	return nil
}
