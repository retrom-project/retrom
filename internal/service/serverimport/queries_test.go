package serverimport

import (
	"context"
	"errors"
	"testing"
)

type queryMemory struct {
	calls   int
	items   ItemQuery
	readErr error
}

func (memory *queryMemory) Get(context.Context, string) (Summary, error) {
	memory.calls++
	return Summary{}, memory.readErr
}

func (memory *queryMemory) List(context.Context, ListQuery) ([]Summary, error) {
	memory.calls++
	return nil, memory.readErr
}

func (memory *queryMemory) Items(_ context.Context, query ItemQuery) ([]Item, error) {
	memory.calls++
	memory.items = query
	return nil, memory.readErr
}

func (memory *queryMemory) Candidates(context.Context, CandidateQuery) ([]Candidate, error) {
	memory.calls++
	return nil, memory.readErr
}

func TestQueriesRejectInvalidBoundsBeforeRepository(t *testing.T) {
	for _, query := range []ListQuery{{Limit: -1}, {Limit: 102}, {State: "unknown", Limit: 20}, {Before: &SummaryCursor{}, Limit: 20}, {Before: &SummaryCursor{ID: "id", CreatedAtMS: -1}, Limit: 20}} {
		memory := &queryMemory{}
		_, err := NewQueries(memory).List(t.Context(), query)
		if !errors.Is(err, ErrQuery) || memory.calls != 0 {
			t.Fatalf("invalid list reached repository: %+v %v", query, err)
		}
	}
}

func TestQueriesRequireCompleteItemAndCandidateCursors(t *testing.T) {
	memory := &queryMemory{}
	service := NewQueries(memory)
	_, itemErr := service.Items(t.Context(), ItemQuery{ImportID: "import", Limit: 20, After: &ItemCursor{ID: "item"}})
	_, candidateErr := service.Candidates(t.Context(), CandidateQuery{ImportID: "import", RequirementID: "item", Limit: 20, After: &CandidateCursor{Rank: 1}})
	if !errors.Is(itemErr, ErrQuery) || !errors.Is(candidateErr, ErrQuery) || memory.calls != 0 {
		t.Fatalf("incomplete cursors: %v %v", itemErr, candidateErr)
	}
}

func TestQueriesNormalizeSearchAndPreserveStorageFailure(t *testing.T) {
	memory := &queryMemory{readErr: context.Canceled}
	service := NewQueries(memory)
	_, err := service.Items(t.Context(), ItemQuery{ImportID: "import", Text: " BIOS ", Limit: 20})
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrQuery) || memory.items.Text != "BIOS" {
		t.Fatalf("item search: %+v %v", memory.items, err)
	}
	memory.readErr = ErrNotFound
	if _, err := service.Get(t.Context(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing summary: %v", err)
	}
}
