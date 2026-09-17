package pegasusimport

import (
	"context"
	"fmt"

	pegasusimportmodel "retrom/internal/model/pegasusimport"
	repository "retrom/internal/repo/pegasusimport"
	pegasusimportservice "retrom/internal/service/pegasusimport"
)

func (service *Service) queries() *pegasusimportservice.Queries {
	return pegasusimportservice.NewQueries(repository.NewQueries(service.database), service.tags)
}

func (service *Service) Get(ctx context.Context, importID string) (Summary, error) {
	value, err := service.queries().Get(ctx, importID)
	return value, queryError(err)
}

func (service *Service) List(
	ctx context.Context,
	state string,
	beforeAt int64,
	beforeID string,
	limit int,
) ([]Summary, error) {
	values, err := service.queries().List(
		ctx,
		pegasusimportmodel.ListQuery{State: state, BeforeAtMS: beforeAt, BeforeID: beforeID, Limit: limit},
	)
	return values, queryError(err)
}

func (service *Service) Collections(
	ctx context.Context,
	importID, afterPath string,
	afterOrdinal int64,
	afterID string,
	limit int,
) ([]Collection, error) {
	values, err := service.queries().Collections(
		ctx,
		pegasusimportmodel.CollectionQuery{
			ImportID:     importID,
			AfterPath:    afterPath,
			AfterOrdinal: afterOrdinal,
			AfterID:      afterID,
			Limit:        limit,
		},
	)
	return values, queryError(err)
}

func (service *Service) Items(
	ctx context.Context,
	importID, query, outcome, warning, collectionID, afterTitle, afterID string,
	limit int,
) ([]Item, error) {
	values, err := service.queries().Items(
		ctx,
		pegasusimportmodel.ItemQuery{
			ImportID:     importID,
			Text:         query,
			Outcome:      outcome,
			Warning:      warning,
			CollectionID: collectionID,
			AfterTitle:   afterTitle,
			AfterID:      afterID,
			Limit:        limit,
		},
	)
	return values, queryError(err)
}

func queryError(err error) error {
	if err != nil {
		return fmt.Errorf("query Pegasus import: %w", err)
	}
	return nil
}
