package libraryimport

import (
	"context"
	"errors"
	"testing"
)

type importReadsRepositoryStub struct {
	summary         ImportOverviewSummary
	list            []ImportListItem
	detail          ImportDetail
	multiDisc       []ImportMultiDiscItemSummary
	history         []ReviewHistoryItem
	historyEvent    ReviewHistoryEvent
	err             error
	listQuery       ImportListQuery
	historyQuery    ReviewHistoryQuery
	lastImportJobID string
	lastEventID     string
}

func (stub *importReadsRepositoryStub) Summary(context.Context) (ImportOverviewSummary, error) {
	return stub.summary, stub.err
}

func (stub *importReadsRepositoryStub) List(_ context.Context, query ImportListQuery) ([]ImportListItem, error) {
	stub.listQuery = query
	return stub.list, stub.err
}

func (stub *importReadsRepositoryStub) Detail(_ context.Context, importJobID string) (ImportDetail, error) {
	stub.lastImportJobID = importJobID
	return stub.detail, stub.err
}

func (stub *importReadsRepositoryStub) MultiDiscItemSummaries(
	_ context.Context,
	importJobID string,
) ([]ImportMultiDiscItemSummary, error) {
	stub.lastImportJobID = importJobID
	return stub.multiDisc, stub.err
}

func (stub *importReadsRepositoryStub) ReviewHistory(
	_ context.Context,
	query ReviewHistoryQuery,
) ([]ReviewHistoryItem, error) {
	stub.historyQuery = query
	return stub.history, stub.err
}

func (stub *importReadsRepositoryStub) ReviewHistoryEvent(
	_ context.Context,
	eventID string,
) (ReviewHistoryEvent, error) {
	stub.lastEventID = eventID
	return stub.historyEvent, stub.err
}

func TestImportReadsRejectsInvalidQueriesBeforeRepository(t *testing.T) {
	t.Parallel()
	stub := &importReadsRepositoryStub{}
	service := NewImportReads(stub)
	if _, err := service.List(t.Context(), ImportListQuery{SortCode: "UPDATED_DESC"}); !errors.Is(err, ErrImportReadQuery) {
		t.Fatalf("list error = %v", err)
	}
	if stub.lastImportJobID != "" || stub.lastEventID != "" {
		t.Fatalf("repository called for invalid query: %#v", stub)
	}
}

func TestImportReadsPassesDecodedListAndHistoryQueries(t *testing.T) {
	t.Parallel()
	stub := &importReadsRepositoryStub{
		list:    []ImportListItem{{ID: "job"}},
		history: []ReviewHistoryItem{{ReviewEventID: "event"}},
	}
	service := NewImportReads(stub)
	query := ImportListQuery{
		QueryText: "hello", State: "COMPLETED", PlatformID: "platform",
		SortCode: "UPDATED_DESC", CursorID: "cursor", CursorValue: 7, Limit: 21,
	}
	items, err := service.List(t.Context(), query)
	if err != nil || len(items) != 1 || stub.listQuery != query {
		t.Fatalf("list = %#v/%v query=%#v", items, err, stub.listQuery)
	}
	history, err := service.ReviewHistory(t.Context(), ReviewHistoryQuery{QueryText: "title", Decision: "APPROVED"})
	if err != nil || len(history) != 1 || stub.historyQuery != (ReviewHistoryQuery{QueryText: "title", Decision: "APPROVED"}) {
		t.Fatalf("history = %#v/%v query=%#v", history, err, stub.historyQuery)
	}
}

func TestImportReadsWrapsRepositoryErrors(t *testing.T) {
	t.Parallel()
	cause := errors.New("storage failed")
	service := NewImportReads(&importReadsRepositoryStub{err: cause})
	if _, err := service.Summary(t.Context()); !errors.Is(err, cause) {
		t.Fatalf("summary error = %v", err)
	}
	if _, err := service.ReviewHistoryEvent(t.Context(), "event"); !errors.Is(err, cause) {
		t.Fatalf("history event error = %v", err)
	}
}
