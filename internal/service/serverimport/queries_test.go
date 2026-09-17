package serverimport

import (
	"context"
	"errors"
	model "retrom/internal/model/serverimport"
	"testing"
)

type queryMemory struct {
	calls   int
	items   model.ItemQuery
	readErr error
}

func (memory *queryMemory) Get(context.Context, string) (model.Summary, error) {
	memory.calls++
	return model.Summary{}, memory.readErr
}

func (memory *queryMemory) List(context.Context, model.ListQuery) ([]model.Summary, error) {
	memory.calls++
	return nil, memory.readErr
}

func (memory *queryMemory) Items(_ context.Context, query model.ItemQuery) ([]model.Item, error) {
	memory.calls++
	memory.items = query
	return nil, memory.readErr
}

func (memory *queryMemory) Candidates(context.Context, model.CandidateQuery) ([]model.Candidate, error) {
	memory.calls++
	return nil, memory.readErr
}

func TestQueriesRejectInvalidBoundsBeforeRepository(t *testing.T) {
	for _, query := range []model.ListQuery{{Limit: -1}, {Limit: 102}, {State: "unknown", Limit: 20}, {Before: &model.SummaryCursor{}, Limit: 20}, {Before: &model.SummaryCursor{ID: "id", CreatedAtMS: -1}, Limit: 20}} {
		memory := &queryMemory{}
		_, err := NewQueries(memory).List(t.Context(), query)
		if !errors.Is(err, model.ErrQuery) || memory.calls != 0 {
			t.Fatalf("invalid list reached repository: %+v %v", query, err)
		}
	}
}

func TestQueriesRequireCompleteItemAndCandidateCursors(t *testing.T) {
	memory := &queryMemory{}
	service := NewQueries(memory)
	_, itemErr := service.Items(t.Context(), model.ItemQuery{ImportID: "import", Limit: 20, After: &model.ItemCursor{ID: "item"}})
	_, candidateErr := service.Candidates(t.Context(), model.CandidateQuery{ImportID: "import", RequirementID: "item", Limit: 20, After: &model.CandidateCursor{Rank: 1}})
	if !errors.Is(itemErr, model.ErrQuery) || !errors.Is(candidateErr, model.ErrQuery) || memory.calls != 0 {
		t.Fatalf("incomplete cursors: %v %v", itemErr, candidateErr)
	}
}

func TestQueriesNormalizeSearchAndPreserveStorageFailure(t *testing.T) {
	memory := &queryMemory{readErr: context.Canceled}
	service := NewQueries(memory)
	_, err := service.Items(t.Context(), model.ItemQuery{ImportID: "import", Text: " BIOS ", Limit: 20})
	if !errors.Is(err, context.Canceled) || errors.Is(err, model.ErrQuery) || memory.items.Text != "BIOS" {
		t.Fatalf("item search: %+v %v", memory.items, err)
	}
	memory.readErr = model.ErrNotFound
	if _, err := service.Get(t.Context(), "missing"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("missing summary: %v", err)
	}
}
