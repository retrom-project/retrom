package libraryimport

import (
	"context"
	"errors"
	"testing"
)

type bulkQueryMemory struct {
	candidates []ReviewBulkCandidate
	query      ReviewBulkCandidateQuery
	calls      int
}

func (memory *bulkQueryMemory) Candidates(_ context.Context, query ReviewBulkCandidateQuery) ([]ReviewBulkCandidate, error) {
	memory.query = query
	memory.calls++
	return memory.candidates, nil
}

func TestReviewBulkQueriesNormalizesScopeBeforeRepository(t *testing.T) {
	t.Parallel()
	jobID := "019b0000-0000-7000-8000-000000000001"
	memory := &bulkQueryMemory{}
	_, err := NewReviewBulkQueries(memory).Candidates(t.Context(), ReviewBulkScope{Q: "  Hello\t界  World \n", ImportJobID: "  " + jobID + " "})
	if err != nil {
		t.Fatal(err)
	}
	if memory.query.Scope.Q != "hello 界 world" || memory.query.Scope.ImportJobID != jobID || memory.query.Limit != ReviewBulkQueryLimit || memory.calls != 1 {
		t.Fatalf("query=%#v calls=%d", memory.query, memory.calls)
	}
}

func TestReviewBulkQueriesRejectsInvalidScopeAndCursorBeforeStorage(t *testing.T) {
	t.Parallel()
	memory := &bulkQueryMemory{}
	service := NewReviewBulkQueries(memory)
	for _, test := range []struct {
		name string
		call func() error
	}{
		{"multiple source filters", func() error {
			_, err := service.Candidates(t.Context(), ReviewBulkScope{ImportJobID: "019b0000-0000-7000-8000-000000000001", SourceImportID: "019b0000-0000-7000-8000-000000000002"})
			return err
		}},
		{"invalid candidate cursor", func() error {
			_, err := service.CandidatesPage(t.Context(), ReviewBulkCandidateQuery{AfterItemID: "wrong", Limit: 1})
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.call()
			if !errors.Is(err, ErrReviewBulkQuery) || memory.calls != 0 {
				t.Fatalf("err=%v calls=%d", err, memory.calls)
			}
		})
	}
}
