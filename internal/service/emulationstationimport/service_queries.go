package emulationstationimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/emulationstationimport"
)

func (service *Service) Get(ctx context.Context, importID string) (model.Summary, error) {
	value, err := service.dependencies.Queries.Get(ctx, importID)
	return value, queryError(err)
}

func (service *Service) List(
	ctx context.Context,
	state string,
	beforeAt int64,
	beforeID string,
	limit int,
) ([]model.Summary, error) {
	values, err := service.dependencies.Queries.List(
		ctx,
		model.ListQuery{State: state, BeforeAtMS: beforeAt, BeforeID: beforeID, Limit: limit},
	)
	return values, queryError(err)
}

func (service *Service) Gamelists(
	ctx context.Context,
	importID, parseState, afterPath string,
	limit int,
) ([]model.Gamelist, error) {
	values, err := service.dependencies.Queries.Gamelists(
		ctx,
		model.GamelistQuery{ImportID: importID, ParseState: parseState, AfterPath: afterPath, Limit: limit},
	)
	return values, queryError(err)
}

func (service *Service) Collections(
	ctx context.Context,
	importID, afterPath, afterID string,
	limit int,
) ([]model.Collection, error) {
	values, err := service.dependencies.Queries.Collections(
		ctx,
		model.CollectionQuery{ImportID: importID, AfterPath: afterPath, AfterID: afterID, Limit: limit},
	)
	return values, queryError(err)
}

func (service *Service) Items(
	ctx context.Context,
	importID, query, outcome, warning, collectionID, afterTitle, afterID string,
	limit int,
) ([]model.Item, error) {
	values, err := service.dependencies.Queries.Items(
		ctx,
		model.ItemQuery{
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
