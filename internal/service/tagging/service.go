package tagging

import (
	"context"
	"fmt"
	"strconv"
	"time"

	model "retrom/internal/model/tagging"

	"github.com/google/uuid"
)

// Options holds injectable capabilities for the tagging service.
type Options struct {
	Now   func() time.Time
	NewID func() (string, error)
}

// Service orchestrates tagging use cases. It validates stateless input,
// prepares command values and delegates persistence to QueryRepository
// and CommandRepository. It never holds a database transaction.
type Service struct {
	queries  model.QueryRepository
	commands model.CommandRepository
	now      func() time.Time
	newID    func() (string, error)
}

// New creates a tagging Service. Nil Options fields use defaults.
func New(queries model.QueryRepository, commands model.CommandRepository, options Options) *Service {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	newID := options.NewID
	if newID == nil {
		newID = func() (string, error) {
			id, err := uuid.NewV7()
			if err != nil {
				return "", fmt.Errorf("tagging: create id: %w", err)
			}
			return id.String(), nil
		}
	}
	return &Service{queries: queries, commands: commands, now: now, newID: newID}
}

func (service *Service) Get(ctx context.Context, tagID string) (model.AdminItem, error) {
	if !model.ValidID(tagID) {
		return model.AdminItem{}, model.ErrNotFound
	}
	item, err := service.queries.Get(ctx, tagID)
	return item, repositoryError("get", err)
}

func (service *Service) List(ctx context.Context, filter model.ListFilter) ([]model.AdminItem, error) {
	if !validListFilter(filter) {
		return nil, model.ErrInvalid
	}
	query := model.ListQuery{
		Status:     filter.Status,
		Sort:       filter.Sort,
		SearchText: model.CanonicalSearch(filter.Query),
		Limit:      filter.Limit,
		AfterID:    filter.AfterID,
	}
	if err := applyListCursor(filter, &query); err != nil {
		return nil, err
	}
	items, err := service.queries.List(ctx, query)
	return items, repositoryError("list", err)
}

func validListFilter(filter model.ListFilter) bool {
	if filter.Status != model.StatusActive && filter.Status != model.StatusDeleted && filter.Status != "ALL" {
		return false
	}
	if filter.Sort != model.SortNameAsc && filter.Sort != model.SortUpdatedDesc {
		return false
	}
	return filter.Limit >= 1 && filter.Limit <= model.MaximumListLimit+1
}

func (service *Service) Summary(ctx context.Context) (model.Summary, error) {
	result, err := service.queries.Summary(ctx)
	return result, repositoryError("summary", err)
}

func (service *Service) references(
	ctx context.Context, kind model.OwnerKind, ids []string,
) (map[string][]model.Reference, error) {
	result, err := service.queries.References(ctx, kind, ids)
	return result, repositoryError("references", err)
}

func (service *Service) References(ctx context.Context, ids []string) (map[string][]model.Reference, error) {
	return service.references(ctx, model.OwnerGame, ids)
}

func (service *Service) ReviewReferences(ctx context.Context, ids []string) (map[string][]model.Reference, error) {
	return service.references(ctx, model.OwnerReviewItem, ids)
}

func (service *Service) PegasusReferences(ctx context.Context, ids []string) (map[string][]model.Reference, error) {
	return service.references(ctx, model.OwnerPegasusCollection, ids)
}

func (service *Service) EmulationStationReferences(
	ctx context.Context, ids []string,
) (map[string][]model.Reference, error) {
	return service.references(ctx, model.OwnerEmulationStationCollection, ids)
}

func applyListCursor(filter model.ListFilter, query *model.ListQuery) error {
	if filter.AfterID == "" {
		return nil
	}
	if !model.ValidID(filter.AfterID) || len(filter.AfterValues) != 1 {
		return model.ErrInvalid
	}
	if filter.Sort == model.SortNameAsc {
		query.AfterNameKey = filter.AfterValues[0]
		return nil
	}
	value, err := strconv.ParseInt(filter.AfterValues[0], 10, 64)
	if err != nil || value < 0 {
		return model.ErrInvalid
	}
	query.AfterUpdatedAt = value
	return nil
}

func repositoryError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("tagging: %s: %w", operation, err)
}
