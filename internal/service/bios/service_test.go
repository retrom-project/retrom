package bios

import (
	"context"
	"errors"
	model "retrom/internal/model/bios"
	"strings"
	"testing"
)

type fakeRepository struct {
	request model.ListRequest
	result  model.ListResult
	err     error
}

func (fake *fakeRepository) List(_ context.Context, request model.ListRequest) (model.ListResult, error) {
	fake.request = request
	return fake.result, fake.err
}

func TestListFetchesOneExtraItemAndBuildsCatalogCursor(t *testing.T) {
	repository := &fakeRepository{result: model.ListResult{Items: []model.Item{
		{ID: "first", CoreName: "Core", LogicalName: "a.bin"},
		{ID: "second", CoreName: "Core", LogicalName: "b.bin"},
	}}}
	service := New(repository)

	result, err := service.List(context.Background(), model.ListRequest{
		Scope: model.ScopeFullCatalog, Quick: model.QuickAll, Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.request.Limit != 2 {
		t.Fatalf("repository limit = %d, want 2", repository.request.Limit)
	}
	if len(result.Items) != 1 || result.Items[0].ID != "first" {
		t.Fatalf("items = %#v", result.Items)
	}
	if result.NextCursor == nil || result.NextCursor.ID != "first" ||
		strings.Join(result.NextCursor.SortValues, "/") != "Core/a.bin" {
		t.Fatalf("next cursor = %#v", result.NextCursor)
	}
}

func TestListKeepsAggregateProjectionWhenThereIsNoNextPage(t *testing.T) {
	repository := &fakeRepository{result: model.ListResult{
		ScopeCounts: model.ScopeCounts{RequiredByLibrary: 2, FullCatalog: 3},
		Summary:     model.Summary{TotalCount: 2, ReadyCount: 1},
		FilteredCount:     1,
		Items:             []model.Item{{ID: "only"}},
	}}
	result, err := New(repository).List(context.Background(), model.ListRequest{
		Scope: model.ScopeRequiredByLibrary, Quick: model.QuickOptional, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.NextCursor != nil || result.ScopeCounts.FullCatalog != 3 ||
		result.Summary.ReadyCount != 1 || result.FilteredCount != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestListRejectsInvalidRequestBeforeRepository(t *testing.T) {
	repository := &fakeRepository{}
	_, err := New(repository).List(context.Background(), model.ListRequest{
		Scope: model.ScopeFullCatalog, Quick: model.QuickAll, Limit: 101,
	})
	if !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
	if repository.request.Limit != 0 {
		t.Fatalf("repository was called with %#v", repository.request)
	}
}

func TestListWrapsRepositoryError(t *testing.T) {
	want := errors.New("read failed")
	_, err := New(&fakeRepository{err: want}).List(context.Background(), model.ListRequest{
		Scope: model.ScopeFullCatalog, Quick: model.QuickAll, Limit: 10,
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped repository error", err)
	}
	if !strings.Contains(err.Error(), "list BIOS catalog") {
		t.Fatalf("error = %v, want operation context", err)
	}
}
