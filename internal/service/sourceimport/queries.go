package sourceimport

import (
	"context"
	"fmt"

	"retrom/internal/service/tagging"
)

type ListQuery struct {
	State      string
	BeforeAtMS int64
	BeforeID   string
	Limit      int
}
type CollectionQuery struct {
	ImportID, AfterPath string
	AfterOrdinal        int64
	AfterID             string
	Limit               int
}
type ItemQuery struct {
	ImportID, Text, Outcome, Warning, CollectionID, AfterTitle, AfterID string
	Limit                                                               int
}
type CollectionRecord struct {
	Collection
	ImportState string
}
type QueryRepository interface {
	Get(context.Context, string) (Summary, error)
	List(context.Context, ListQuery) ([]Summary, error)
	Collections(context.Context, CollectionQuery) ([]CollectionRecord, error)
	Items(context.Context, ItemQuery) ([]Item, error)
}
type CollectionTags interface {
	SourceReferences(context.Context, []string) (map[string][]tagging.Reference, error)
}
type Queries struct {
	repository QueryRepository
	tags       CollectionTags
}

func NewQueries(repository QueryRepository, tags CollectionTags) *Queries {
	return &Queries{repository: repository, tags: tags}
}

func (service *Queries) Get(ctx context.Context, id string) (Summary, error) {
	value, err := service.repository.Get(ctx, id)
	if err != nil {
		return Summary{}, fmt.Errorf("get Source import: %w", err)
	}
	return value, nil
}

func (service *Queries) List(ctx context.Context, query ListQuery) ([]Summary, error) {
	if query.Limit < 1 || query.Limit > 21 {
		return nil, ErrInvalid
	}
	values, err := service.repository.List(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list Source imports: %w", err)
	}
	return values, nil
}

func (service *Queries) Items(ctx context.Context, query ItemQuery) ([]Item, error) {
	if query.Limit < 1 || query.Limit > 51 {
		return nil, ErrInvalid
	}
	values, err := service.repository.Items(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list Source items: %w", err)
	}
	return values, nil
}

func (service *Queries) Collections(ctx context.Context, query CollectionQuery) ([]Collection, error) {
	if query.Limit < 1 || query.Limit > 101 {
		return nil, ErrInvalid
	}
	records, err := service.repository.Collections(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list Source collections: %w", err)
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		if record.ImportState == "AWAITING_MAPPING" {
			ids = append(ids, record.ID)
		}
	}
	var references map[string][]tagging.Reference
	if len(ids) > 0 {
		references, err = service.tags.SourceReferences(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("read Source mapping tags: %w", err)
		}
	}
	values := make([]Collection, 0, len(records))
	for _, record := range records {
		value := record.Collection
		if record.ImportState == "AWAITING_MAPPING" {
			value.TagSnapshot = append([]tagging.Reference{}, references[record.ID]...)
		}
		values = append(values, value)
	}
	return values, nil
}
