package emulationstationimport

import (
	"context"
	"fmt"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	persistence "retrom/internal/repo/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
)

func (service *Service) queries() *emulationstationimportservice.Queries {
	return emulationstationimportservice.NewQueries(persistence.NewQueries(service.database), service.tags)
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
		emulationstationimportmodel.ListQuery{State: state, BeforeAtMS: beforeAt, BeforeID: beforeID, Limit: limit},
	)
	return values, queryError(err)
}

func (service *Service) Gamelists(
	ctx context.Context,
	importID, parseState, afterPath string,
	limit int,
) ([]Gamelist, error) {
	values, err := service.queries().Gamelists(
		ctx,
		emulationstationimportmodel.GamelistQuery{ImportID: importID, ParseState: parseState, AfterPath: afterPath, Limit: limit},
	)
	return values, queryError(err)
}

func (service *Service) Collections(
	ctx context.Context,
	importID, afterPath, afterID string,
	limit int,
) ([]Collection, error) {
	values, err := service.queries().Collections(
		ctx,
		emulationstationimportmodel.CollectionQuery{ImportID: importID, AfterPath: afterPath, AfterID: afterID, Limit: limit},
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
		emulationstationimportmodel.ItemQuery{
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

func mediaProjection(present bool, warnings []map[string]any, field string) string {
	return emulationstationimportmodel.ProjectMedia(present, warnings, field)
}

func queryError(err error) error {
	if err != nil {
		return fmt.Errorf("query EmulationStation import: %w", err)
	}
	return nil
}
