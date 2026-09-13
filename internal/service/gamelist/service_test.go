package gamelist

import (
	"context"
	"errors"
	"testing"
)

type fakeRepository struct {
	detail  Detail
	page    ListResult
	err     error
	request ListRequest
}

func (repository *fakeRepository) Detail(context.Context, string, string) (Detail, error) {
	return repository.detail, repository.err
}

func (repository *fakeRepository) List(_ context.Context, request ListRequest) (ListResult, error) {
	repository.request = request
	return repository.page, repository.err
}

func TestListTrimsOverflowAndBuildsCursor(t *testing.T) {
	repository := &fakeRepository{page: ListResult{Items: []GameItem{
		{ID: "game-1", Title: "First", CreatedAtMS: 100, UpdatedAtMS: 110},
		{ID: "game-2", Title: "Second", CreatedAtMS: 200, UpdatedAtMS: 210},
		{ID: "game-3", Title: "Third", CreatedAtMS: 300, UpdatedAtMS: 310},
	}}}
	service := New(repository)
	result, err := service.List(context.Background(), ListRequest{
		ProfileID: "profile",
		Sort:      SortAddedDesc,
		Limit:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 || result.Items[1].ID != "game-2" {
		t.Fatalf("items = %#v", result.Items)
	}
	if result.NextCursor == nil || result.NextCursor.ID != "game-2" {
		t.Fatalf("next cursor = %#v", result.NextCursor)
	}
	if got, want := result.NextCursor.SortValues, []string{"200", "Second"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("cursor values = %#v, want %#v", got, want)
	}
	if repository.request.Limit != 3 {
		t.Fatalf("repository limit = %d, want 3", repository.request.Limit)
	}
}

func TestListBuildsRecentCursorWithMissingPlayTime(t *testing.T) {
	repository := &fakeRepository{page: ListResult{Items: []GameItem{
		{ID: "game-1", Title: "First", CreatedAtMS: 100},
		{ID: "game-2", Title: "Second", CreatedAtMS: 200},
	}}}
	result, err := New(repository).List(context.Background(), ListRequest{
		ProfileID: "profile",
		Sort:      SortRecentDesc,
		Limit:     1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.NextCursor == nil {
		t.Fatal("expected next cursor")
	}
	want := []string{"-1", "100", "First"}
	for index, value := range want {
		if result.NextCursor.SortValues[index] != value {
			t.Fatalf("cursor values = %#v, want %#v", result.NextCursor.SortValues, want)
		}
	}
}

func TestListRejectsInvalidRequest(t *testing.T) {
	for name, request := range map[string]ListRequest{
		"missing profile": {Sort: SortTitleAsc, Limit: 1},
		"missing limit":   {ProfileID: "profile", Sort: SortTitleAsc},
		"missing sort":    {ProfileID: "profile", Limit: 1},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := New(&fakeRepository{}).List(context.Background(), request)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want %v", err, ErrInvalid)
			}
		})
	}
}

func TestDetailPropagatesNotFound(t *testing.T) {
	repository := &fakeRepository{err: ErrNotFound}
	_, err := New(repository).Detail(context.Background(), "profile", "game")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want %v", err, ErrNotFound)
	}
}
