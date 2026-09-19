package pegasusimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/pegasusimport"
	taggingmodel "retrom/internal/model/tagging"
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
		return model.Summary{}, fmt.Errorf("get Pegasus import: %w", err)
	}
	return value, nil
}

func (service *Queries) List(ctx context.Context, query model.ListQuery) ([]model.Summary, error) {
	if query.Limit < 1 || query.Limit > 21 {
		return nil, model.ErrInvalid
	}
	values, err := service.repository.List(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list Pegasus imports: %w", err)
	}
	return values, nil
}

func (service *Queries) Items(ctx context.Context, query model.ItemQuery) ([]model.Item, error) {
	if query.Limit < 1 || query.Limit > 51 {
		return nil, model.ErrInvalid
	}
	values, err := service.repository.Items(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list Pegasus items: %w", err)
	}
	return values, nil
}

func (service *Queries) Collections(ctx context.Context, query model.CollectionQuery) ([]model.Collection, error) {
	if query.Limit < 1 || query.Limit > 101 {
		return nil, model.ErrInvalid
	}
	records, err := service.repository.Collections(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list Pegasus collections: %w", err)
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		if record.ImportState == "AWAITING_MAPPING" {
			ids = append(ids, record.ID)
		}
	}
	var references map[string][]taggingmodel.Reference
	if len(ids) > 0 {
		references, err = service.tags.PegasusReferences(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("read Pegasus mapping tags: %w", err)
		}
	}
	values := make([]model.Collection, 0, len(records))
	for _, record := range records {
		value := record.Collection
		if record.ImportState == "AWAITING_MAPPING" {
			value.TagSnapshot = append([]taggingmodel.Reference{}, references[record.ID]...)
		}
		values = append(values, value)
	}
	return values, nil
}
