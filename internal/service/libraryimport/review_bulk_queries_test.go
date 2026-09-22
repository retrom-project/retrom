package libraryimport

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type bulkQueryMemory struct {
	candidates []ReviewBulkCandidate
	items      []ReviewBulkItemRecord
	summary    ReviewBulkSummary
	active     *ReviewBulkSummary
	query      ReviewBulkCandidateQuery
	itemQuery  ReviewBulkItemQuery
	err        error
	calls      int
}

func (memory *bulkQueryMemory) Candidates(_ context.Context, query ReviewBulkCandidateQuery) ([]ReviewBulkCandidate, error) {
	memory.query = query
	memory.calls++
	return memory.candidates, memory.err
}

func (memory *bulkQueryMemory) Items(_ context.Context, query ReviewBulkItemQuery) ([]ReviewBulkItemRecord, error) {
	memory.itemQuery = query
	memory.calls++
	return memory.items, memory.err
}

func (memory *bulkQueryMemory) Summary(context.Context, string) (ReviewBulkSummary, error) {
	memory.calls++
	return memory.summary, memory.err
}

func (memory *bulkQueryMemory) ActiveSummary(context.Context) (ReviewBulkSummary, bool, error) {
	memory.calls++
	if memory.active == nil {
		return ReviewBulkSummary{}, false, memory.err
	}
	return *memory.active, true, memory.err
}

func TestReviewBulkQueriesNormalizesScopeBeforeRepository(t *testing.T) {
	t.Parallel()
	jobID := "019b0000-0000-7000-8000-000000000001"
	memory := &bulkQueryMemory{}
	_, err := NewReviewBulkQueries(memory).Candidates(t.Context(), ReviewBulkScope{
		Q: "  Hello\t界  World \n", ImportJobID: "  " + jobID + " ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if memory.query.Scope.Q != "hello 界 world" || memory.query.Scope.ImportJobID != jobID ||
		memory.query.Limit != ReviewBulkQueryLimit || memory.calls != 1 {
		t.Fatalf("query=%#v calls=%d", memory.query, memory.calls)
	}
}

func TestReviewBulkQueriesRejectInvalidScopeAndCursorBeforeStorage(t *testing.T) {
	t.Parallel()
	memory := &bulkQueryMemory{}
	service := NewReviewBulkQueries(memory)
	for _, test := range []struct {
		name string
		call func() error
	}{
		{"multiple source filters", func() error {
			_, err := service.Candidates(t.Context(), ReviewBulkScope{
				ImportJobID:    "019b0000-0000-7000-8000-000000000001",
				SourceImportID: "019b0000-0000-7000-8000-000000000002",
			})
			return err
		}},
		{"invalid candidate cursor", func() error {
			_, err := service.CandidatesPage(t.Context(), ReviewBulkCandidateQuery{AfterItemID: "wrong", Limit: 1})
			return err
		}},
		{"invalid item cursor", func() error {
			_, err := service.Items(t.Context(), "019b0000-0000-7000-8000-000000000003", "", "-1", 1)
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

func TestReviewBulkQueriesUsesLookaheadForItems(t *testing.T) {
	t.Parallel()
	bulkID := "019b0000-0000-7000-8000-000000000003"
	memory := &bulkQueryMemory{items: []ReviewBulkItemRecord{
		{ImportItemID: "first", Ordinal: 3},
		{ImportItemID: "second", Ordinal: 5},
		{ImportItemID: "lookahead", Ordinal: 7},
	}}
	page, err := NewReviewBulkQueries(memory).Items(t.Context(), bulkID, "PUBLISHED", "2", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(page.Items, memory.items[:2]) || page.NextCursor == nil || *page.NextCursor != "5" {
		t.Fatalf("page=%#v", page)
	}
	if memory.itemQuery != (ReviewBulkItemQuery{BulkApprovalID: bulkID, Outcome: "PUBLISHED", AfterOrdinal: 2, Limit: 3}) {
		t.Fatalf("item query=%#v", memory.itemQuery)
	}
}

func TestReviewBulkCandidateManifestDigestIsOrderIndependent(t *testing.T) {
	t.Parallel()
	validation := "validation"
	one := []ReviewBulkCandidate{
		{ItemID: "b", ReviewVersion: 2, ValidationID: &validation, SourceSnapshotID: "snapshot-b"},
		{ItemID: "a", ReviewVersion: 1, ValidationID: &validation, SourceSnapshotID: "snapshot-a"},
	}
	two := []ReviewBulkCandidate{one[1], one[0]}
	if left, right := ReviewBulkCandidateManifestDigest(one), ReviewBulkCandidateManifestDigest(two); left != right {
		t.Fatalf("digest changed with order: %s != %s", left, right)
	}
	if got := ReviewBulkCandidateManifestDigest([]ReviewBulkCandidate{{ItemID: strings.Repeat("a", 1)}}); got == "" {
		t.Fatal("empty manifest digest")
	}
}
