package serverimport

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNotFound = errors.New("SERVER_IMPORT_NOT_FOUND")
	ErrQuery    = errors.New("SERVER_IMPORT_QUERY_INVALID")
)

type SummaryCursor struct {
	CreatedAtMS int64
	ID          string
}
type (
	ItemCursor      struct{ Core, Name, ID string }
	CandidateCursor struct {
		Rank int64
		ID   string
	}
)

type ListQuery struct {
	State  string
	Before *SummaryCursor
	Limit  int
}
type ItemQuery struct {
	ImportID, Text, Outcome, Method string
	After                           *ItemCursor
	Limit                           int
}
type CandidateQuery struct {
	ImportID, RequirementID string
	After                   *CandidateCursor
	Limit                   int
}
type QueryRepository interface {
	Get(context.Context, string) (Summary, error)
	List(context.Context, ListQuery) ([]Summary, error)
	Items(context.Context, ItemQuery) ([]Item, error)
	Candidates(context.Context, CandidateQuery) ([]Candidate, error)
}
type Queries struct{ repository QueryRepository }

func NewQueries(repository QueryRepository) *Queries { return &Queries{repository} }
func (service *Queries) Get(ctx context.Context, id string) (Summary, error) {
	if id == "" {
		return Summary{}, ErrQuery
	}
	value, err := service.repository.Get(ctx, id)
	if err != nil {
		return Summary{}, fmt.Errorf("read server import summary: %w", err)
	}
	return value, nil
}

func (service *Queries) List(ctx context.Context, query ListQuery) ([]Summary, error) {
	if query.Limit < 1 || query.Limit > 101 || !ValidState(query.State) {
		return nil, ErrQuery
	}
	if query.Before != nil && (query.Before.ID == "" || query.Before.CreatedAtMS < 0) {
		return nil, ErrQuery
	}
	result, err := service.repository.List(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list server import summaries: %w", err)
	}
	return result, nil
}

func (service *Queries) Items(ctx context.Context, query ItemQuery) ([]Item, error) {
	if query.ImportID == "" || query.Limit < 1 || query.Limit > 101 {
		return nil, ErrQuery
	}
	if query.After != nil && (query.After.ID == "" || query.After.Core == "" || query.After.Name == "") {
		return nil, ErrQuery
	}
	query.Text = strings.TrimSpace(query.Text)
	result, err := service.repository.Items(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list server import items: %w", err)
	}
	return result, nil
}

func (service *Queries) Candidates(ctx context.Context, query CandidateQuery) ([]Candidate, error) {
	if query.ImportID == "" || query.RequirementID == "" || query.Limit < 1 || query.Limit > 101 {
		return nil, ErrQuery
	}
	if query.After != nil && (query.After.ID == "" || query.After.Rank < 1) {
		return nil, ErrQuery
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
