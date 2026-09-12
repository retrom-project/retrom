package serverimport

import (
	"context"
	"fmt"

	importpersistence "retrom/internal/persistence/serverimport"
	importservice "retrom/internal/service/serverimport"
)

func (service *Service) queries() *importservice.Queries {
	return importservice.NewQueries(importpersistence.NewQueries(service.database))
}

func (service *Service) Get(ctx context.Context, id string) (Summary, error) {
	result, err := service.queries().Get(ctx, id)
	if err != nil {
		return Summary{}, fmt.Errorf("query server import: %w", err)
	}
	return result, nil
}

func (service *Service) List(
	ctx context.Context,
	state string,
	beforeAt int64,
	beforeID string,
	limit int,
) ([]Summary, error) {
	query := importservice.ListQuery{State: state, Limit: limit}
	if beforeAt != 0 || beforeID != "" {
		query.Before = &importservice.SummaryCursor{CreatedAtMS: beforeAt, ID: beforeID}
	}
	result, err := service.queries().List(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query server import history: %w", err)
	}
	return result, nil
}

func (service *Service) Items(
	ctx context.Context,
	importID, query, outcome, method, afterCore, afterName, afterID string,
	limit int,
) ([]Item, error) {
	filter := importservice.ItemQuery{ImportID: importID, Text: query, Outcome: outcome, Method: method, Limit: limit}
	if afterCore != "" || afterName != "" || afterID != "" {
		filter.After = &importservice.ItemCursor{Core: afterCore, Name: afterName, ID: afterID}
	}
	result, err := service.queries().Items(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("query server import items: %w", err)
	}
	return result, nil
}

func (service *Service) Candidates(
	ctx context.Context,
	importID, requirementID string,
	afterRank int64,
	afterID string,
	limit int,
) ([]Candidate, error) {
	filter := importservice.CandidateQuery{ImportID: importID, RequirementID: requirementID, Limit: limit}
	if afterRank != 0 || afterID != "" {
		filter.After = &importservice.CandidateCursor{Rank: afterRank, ID: afterID}
	}
	result, err := service.queries().Candidates(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("query server import candidates: %w", err)
	}
	return result, nil
}
