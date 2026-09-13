package emulationstationimport

import (
	"context"
	"fmt"
)

func (service *Service) Get(ctx context.Context, importID string) (Summary, error) {
	value, err := service.dependencies.Queries.Get(ctx, importID)
	return value, queryError(err)
}

func (service *Service) List(
	ctx context.Context,
	state string,
	beforeAt int64,
	beforeID string,
	limit int,
) ([]Summary, error) {
	values, err := service.dependencies.Queries.List(
		ctx,
		ListQuery{State: state, BeforeAtMS: beforeAt, BeforeID: beforeID, Limit: limit},
	)
	return values, queryError(err)
}

func (service *Service) Gamelists(
	ctx context.Context,
	importID, parseState, afterPath string,
	limit int,
) ([]Gamelist, error) {
	values, err := service.dependencies.Queries.Gamelists(
		ctx,
		GamelistQuery{ImportID: importID, ParseState: parseState, AfterPath: afterPath, Limit: limit},
	)
	return values, queryError(err)
}

func (service *Service) Collections(
	ctx context.Context,
	importID, afterPath, afterID string,
	limit int,
) ([]Collection, error) {
	values, err := service.dependencies.Queries.Collections(
		ctx,
		CollectionQuery{ImportID: importID, AfterPath: afterPath, AfterID: afterID, Limit: limit},
	)
	return values, queryError(err)
}

func (service *Service) Items(
	ctx context.Context,
	importID, query, outcome, warning, collectionID, afterTitle, afterID string,
	limit int,
) ([]Item, error) {
	values, err := service.dependencies.Queries.Items(
		ctx,
		ItemQuery{
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
		return fmt.Errorf("query EmulationStation import: %w", err)
	}
	return nil
}
