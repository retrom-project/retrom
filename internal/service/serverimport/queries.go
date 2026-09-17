package serverimport

import (
	"context"
	"fmt"
	"strings"

	model "retrom/internal/model/serverimport"
)

type QueryRepository interface {
	Get(context.Context, string) (model.Summary, error)
	List(context.Context, model.ListQuery) ([]model.Summary, error)
	Items(context.Context, model.ItemQuery) ([]model.Item, error)
	Candidates(context.Context, model.CandidateQuery) ([]model.Candidate, error)
}
type Queries struct{ repository QueryRepository }

func NewQueries(repository QueryRepository) *Queries { return &Queries{repository} }
func (service *Queries) Get(ctx context.Context, id string) (model.Summary, error) {
	if id == "" {
		return model.Summary{}, model.ErrQuery
	}
	value, err := service.repository.Get(ctx, id)
	if err != nil {
		return model.Summary{}, fmt.Errorf("read server import summary: %w", err)
	}
	return value, nil
}

func (service *Queries) List(ctx context.Context, query model.ListQuery) ([]model.Summary, error) {
	if query.Limit < 1 || query.Limit > 101 || !ValidState(query.State) {
		return nil, model.ErrQuery
	}
	if query.Before != nil && (query.Before.ID == "" || query.Before.CreatedAtMS < 0) {
		return nil, model.ErrQuery
	}
	result, err := service.repository.List(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list server import summaries: %w", err)
	}
	return result, nil
}

func (service *Queries) Items(ctx context.Context, query model.ItemQuery) ([]model.Item, error) {
	if query.ImportID == "" || query.Limit < 1 || query.Limit > 101 {
		return nil, model.ErrQuery
	}
	if query.After != nil && (query.After.ID == "" || query.After.Core == "" || query.After.Name == "") {
		return nil, model.ErrQuery
	}
	query.Text = strings.TrimSpace(query.Text)
	result, err := service.repository.Items(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list server import items: %w", err)
	}
	return result, nil
}

func (service *Queries) Candidates(ctx context.Context, query model.CandidateQuery) ([]model.Candidate, error) {
	if query.ImportID == "" || query.RequirementID == "" || query.Limit < 1 || query.Limit > 101 {
		return nil, model.ErrQuery
	}
	if query.After != nil && (query.After.ID == "" || query.After.Rank < 1) {
		return nil, model.ErrQuery
	}
	result, err := service.repository.Candidates(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list server import candidates: %w", err)
	}
	return result, nil
}

func ValidState(state string) bool {
	switch state {
	case "", "QUEUED", "RUNNING", "COMPLETED", "PARTIAL_FAILURE", "CANCEL_REQUESTED", "CANCELLED", "FAILED":
		return true
	}
	return false
}
