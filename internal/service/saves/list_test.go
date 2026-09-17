package saves

import (
	"context"
	"errors"
	"testing"

	model "retrom/internal/model/saves"
)

type saveReadStub struct {
	model.Repository
	items []model.ListItem
	err   error
	query model.ListQuery
}

func (stub *saveReadStub) List(_ context.Context, query model.ListQuery) ([]model.ListItem, error) {
	stub.query = query
	return stub.items, stub.err
}

type saveMutationStub struct {
	saveReadStub
	rename model.RenameRequest
	delete model.DeleteRequest
	err    error
}

func (stub *saveMutationStub) Rename(_ context.Context, request model.RenameRequest) error {
	stub.rename = request
	return stub.err
}

func (stub *saveMutationStub) Delete(_ context.Context, request model.DeleteRequest) error {
	stub.delete = request
	return stub.err
}

func TestListDelegatesTypedQueryAndWrapsErrors(t *testing.T) {
	cause := errors.New("query failed")
	stub := &saveReadStub{err: cause}
	service := New(stub, nil, nil)
	query := model.ListQuery{ProfileID: "profile", Availability: "AVAILABLE", Limit: 3}
	_, err := service.List(t.Context(), query)
	if !errors.Is(err, cause) {
		t.Fatalf("list error lost cause: %v", err)
	}
	if stub.query != query {
		t.Fatalf("list query=%+v want=%+v", stub.query, query)
	}
}

func TestSaveMutationsDelegateTypedRequests(t *testing.T) {
	stub := &saveMutationStub{}
	service := New(stub, nil, nil)
	rename := model.RenameRequest{SaveStateID: "save", ProfileID: "profile", Name: "renamed", ExpectedVersion: 2, UpdatedAtMS: 9}
	if err := service.Rename(t.Context(), rename); err != nil {
		t.Fatal(err)
	}
	if stub.rename != rename {
		t.Fatalf("rename request=%+v want=%+v", stub.rename, rename)
	}
	remove := model.DeleteRequest{SaveStateID: "save", ProfileID: "profile", ExpectedVersion: 3, UpdatedAtMS: 10}
	if err := service.Delete(t.Context(), remove); err != nil {
		t.Fatal(err)
	}
	if stub.delete != remove {
		t.Fatalf("delete request=%+v want=%+v", stub.delete, remove)
	}
}
