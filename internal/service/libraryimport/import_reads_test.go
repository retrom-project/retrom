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
	err             error
	listQuery       ImportListQuery
	lastImportJobID string
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

func TestImportReadsRejectsInvalidQueriesBeforeRepository(t *testing.T) {
	t.Parallel()
	stub := &importReadsRepositoryStub{}
	service := NewImportReads(stub)
	if _, err := service.List(t.Context(), ImportListQuery{SortCode: "UPDATED_DESC"}); !errors.Is(err, ErrImportReadQuery) {
		t.Fatalf("list error = %v", err)
	}
	if stub.lastImportJobID != "" {
		t.Fatalf("repository called for invalid query: %#v", stub)
	}
}

func TestImportReadsPassesDecodedListQueries(t *testing.T) {
	t.Parallel()
	stub := &importReadsRepositoryStub{
		list: []ImportListItem{{ID: "job"}},
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
}

func TestImportReadsWrapsRepositoryErrors(t *testing.T) {
	t.Parallel()
	cause := errors.New("storage failed")
	service := NewImportReads(&importReadsRepositoryStub{err: cause})
	if _, err := service.Summary(t.Context()); !errors.Is(err, cause) {
		t.Fatalf("summary error = %v", err)
	}
}
