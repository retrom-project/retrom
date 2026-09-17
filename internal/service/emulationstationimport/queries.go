package emulationstationimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/emulationstationimport"

	"retrom/internal/model/tagging"
)

type Queries struct {
	repository model.QueryRepository
	tags       model.CollectionTags
}

func NewQueries(repository model.QueryRepository, tags model.CollectionTags) *Queries {
	return &Queries{repository: repository, tags: tags}
}

func (service *Queries) Get(ctx context.Context, id string) (model.Summary, error) {
	value, err := service.repository.Get(ctx, id)
	if err != nil {
		return model.Summary{}, fmt.Errorf("get EmulationStation import: %w", err)
	}
	return value, nil
}

func (service *Queries) List(ctx context.Context, query model.ListQuery) ([]model.Summary, error) {
	if query.Limit < 1 || query.Limit > 21 {
		return nil, model.ErrInvalid
	}
	values, err := service.repository.List(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list EmulationStation imports: %w", err)
	}
	return values, nil
}

func (service *Queries) Items(ctx context.Context, query model.ItemQuery) ([]model.Item, error) {
	if query.Limit < 1 || query.Limit > 51 {
		return nil, model.ErrInvalid
	}
	values, err := service.repository.Items(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list EmulationStation items: %w", err)
	}
	return values, nil
}

func (service *Queries) Collections(ctx context.Context, query model.CollectionQuery) ([]model.Collection, error) {
	if query.Limit < 1 || query.Limit > 101 {
		return nil, model.ErrInvalid
	}
	records, err := service.repository.Collections(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list EmulationStation collections: %w", err)
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		if record.ImportState == "AWAITING_MAPPING" {
			ids = append(ids, record.ID)
		}
	}
	var references map[string][]tagging.Reference
	if len(ids) > 0 {
		references, err = service.tags.EmulationStationReferences(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("read EmulationStation mapping tags: %w", err)
		}
	}
	values := make([]model.Collection, 0, len(records))
	for _, record := range records {
		value := record.Collection
		if record.ImportState == "AWAITING_MAPPING" {
			value.TagSnapshot = append([]tagging.Reference{}, references[record.ID]...)
		}
		values = append(values, value)
	}
	return values, nil
}

func (service *Queries) Gamelists(ctx context.Context, query model.GamelistQuery) ([]model.Gamelist, error) {
	if query.Limit < 1 || query.Limit > 101 {
		return nil, model.ErrInvalid
	}
	values, err := service.repository.Gamelists(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list EmulationStation gamelists: %w", err)
	}
	return values, nil
}
