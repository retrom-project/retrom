package serverimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/serverimport"
)

func (service *Service) Get(ctx context.Context, id string) (model.Summary, error) {
	result, err := service.queries.Get(ctx, id)
	if err != nil {
		return model.Summary{}, fmt.Errorf("query server import: %w", err)
	}
	return result, nil
}

func (service *Service) List(
	ctx context.Context,
	state string,
	beforeAt int64,
	beforeID string,
	limit int,
) ([]model.Summary, error) {
	query := model.ListQuery{State: state, Limit: limit}
	if beforeAt != 0 || beforeID != "" {
		query.Before = &model.SummaryCursor{CreatedAtMS: beforeAt, ID: beforeID}
	}
	result, err := service.queries.List(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query server import history: %w", err)
	}
	return result, nil
}

func (service *Service) Items(
	ctx context.Context,
	importID, query, outcome, method, afterCore, afterName, afterID string,
	limit int,
) ([]model.Item, error) {
	filter := model.ItemQuery{ImportID: importID, Text: query, Outcome: outcome, Method: method, Limit: limit}
	if afterCore != "" || afterName != "" || afterID != "" {
		filter.After = &model.ItemCursor{Core: afterCore, Name: afterName, ID: afterID}
	}
	result, err := service.queries.Items(ctx, filter)
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
) ([]model.Candidate, error) {
	filter := model.CandidateQuery{ImportID: importID, RequirementID: requirementID, Limit: limit}
	if afterRank != 0 || afterID != "" {
		filter.After = &model.CandidateCursor{Rank: afterRank, ID: afterID}
	}
	result, err := service.queries.Candidates(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("query server import candidates: %w", err)
	}
	return result, nil
}
