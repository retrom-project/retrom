package emulationstationimport

import (
	"context"
	"errors"
	"reflect"
	model "retrom/internal/model/emulationstationimport"
	"testing"

	"retrom/internal/model/tagging"
)

var errQueryTest = errors.New("query failed")

type queryMemory struct {
	called      int
	err         error
	list        model.ListQuery
	collections model.CollectionQuery
	items       model.ItemQuery
	gamelists   model.GamelistQuery
	records     []model.CollectionRecord
}

func (m *queryMemory) Get(context.Context, string) (model.Summary, error) {
	m.called++
	return model.Summary{ID: "partial"}, m.err
}

func (m *queryMemory) List(_ context.Context, q model.ListQuery) ([]model.Summary, error) {
	m.called++
	m.list = q
	return []model.Summary{}, m.err
}

func (m *queryMemory) Collections(_ context.Context, q model.CollectionQuery) ([]model.CollectionRecord, error) {
	m.called++
	m.collections = q
	return m.records, m.err
}

func (m *queryMemory) Items(_ context.Context, q model.ItemQuery) ([]model.Item, error) {
	m.called++
	m.items = q
	return []model.Item{}, m.err
}

func (m *queryMemory) Gamelists(_ context.Context, q model.GamelistQuery) ([]model.Gamelist, error) {
	m.called++
	m.gamelists = q
	return []model.Gamelist{{RelativePath: "partial"}}, m.err
}

type tagMemory struct {
	ids    []string
	err    error
	called int
	values map[string][]tagging.Reference
}

func (m *tagMemory) EmulationStationReferences(
	_ context.Context,
	ids []string,
) (map[string][]tagging.Reference, error) {
	m.called++
	m.ids = ids
	return m.values, m.err
}

func TestQueriesRejectLimitsBeforeReading(t *testing.T) {
	t.Parallel()
	for _, limit := range []int{-1, 0, 102} {
		t.Run(string(rune(limit+1000)), func(t *testing.T) {
			t.Parallel()
			repo := &queryMemory{}
			service := NewQueries(repo, &tagMemory{})
			_, a := service.List(t.Context(), model.ListQuery{Limit: limit})
			_, b := service.Collections(t.Context(), model.CollectionQuery{Limit: limit})
			_, c := service.Items(t.Context(), model.ItemQuery{Limit: limit})
			_, d := service.Gamelists(t.Context(), model.GamelistQuery{Limit: limit})
			for _, err := range []error{a, b, c, d} {
				if !errors.Is(err, model.ErrInvalid) {
					t.Fatalf("limit %d: %v", limit, err)
				}
			}
			if repo.called != 0 {
				t.Fatal("invalid query reached repository")
			}
		})
	}
}

func TestQueriesPreserveTypedFiltersAndBoundaries(t *testing.T) {
	t.Parallel()
	repo := &queryMemory{}
	service := NewQueries(repo, &tagMemory{})
	list := model.ListQuery{State: "RUNNING", BeforeAtMS: 123, BeforeID: "before", Limit: 21}
	collections := model.CollectionQuery{ImportID: "import", AfterPath: "metadata", AfterID: "collection", Limit: 101}
	items := model.ItemQuery{
		ImportID:     "import",
		Text:         "title",
		Outcome:      "COMMIT_FAILED",
		Warning:      "MEDIA",
		CollectionID: "collection",
		AfterTitle:   "after",
		AfterID:      "item",
		Limit:        51,
	}
	if _, err := service.List(t.Context(), list); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Collections(t.Context(), collections); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Items(t.Context(), items); err != nil {
		t.Fatal(err)
	}
	gamelists := model.GamelistQuery{ImportID: "import", ParseState: "INVALID", AfterPath: "gamelist.xml", Limit: 101}
	if _, err := service.Gamelists(t.Context(), gamelists); err != nil {
		t.Fatal(err)
	}
	if repo.gamelists != gamelists {
		t.Fatalf("gamelist filter changed: %#v", repo.gamelists)
	}
	if repo.list != list || repo.collections != collections || repo.items != items {
		t.Fatalf("filters changed: %#v", repo)
	}
	for _, err := range queryOverflows(t, service) {
		if !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("overflow: %v", err)
		}
	}
}

func queryOverflows(t *testing.T, service *Queries) []error {
	t.Helper()
	_, a := service.List(t.Context(), model.ListQuery{Limit: 22})
	_, b := service.Items(t.Context(), model.ItemQuery{Limit: 52})
	_, c := service.Gamelists(t.Context(), model.GamelistQuery{Limit: 102})
	return []error{a, b, c}
}

func TestQueriesPreserveErrorsWithoutPartialResults(t *testing.T) {
	t.Parallel()
	repo := &queryMemory{err: errQueryTest}
	service := NewQueries(repo, &tagMemory{})
	summary, a := service.Get(t.Context(), "import")
	list, b := service.List(t.Context(), model.ListQuery{Limit: 1})
	collections, c := service.Collections(t.Context(), model.CollectionQuery{Limit: 1})
	items, d := service.Items(t.Context(), model.ItemQuery{Limit: 1})
	gamelists, e := service.Gamelists(t.Context(), model.GamelistQuery{Limit: 1})
	for _, err := range []error{a, b, c, d, e} {
		if !errors.Is(err, errQueryTest) {
			t.Fatalf("lost cause: %v", err)
		}
	}
	if summary.ID != "" || list != nil || collections != nil || items != nil || gamelists != nil {
		t.Fatal("failed queries leaked partial results")
	}
}

func TestCollectionTagsUseLiveMappingAndPreserveFrozenSelection(t *testing.T) {
	t.Parallel()
	frozen := []tagging.Reference{{TagID: "old", Name: "Frozen"}}
	live := []tagging.Reference{{TagID: "active", Name: "Live"}}
	repo := &queryMemory{
		records: []model.CollectionRecord{
			{Collection: model.Collection{ID: "mapping", TagSnapshot: frozen}, ImportState: "AWAITING_MAPPING"},
			{Collection: model.Collection{ID: "frozen", TagSnapshot: frozen}, ImportState: "RUNNING"},
			{Collection: model.Collection{ID: "deleted", TagSnapshot: frozen}, ImportState: "AWAITING_MAPPING"},
		},
	}
	tags := &tagMemory{values: map[string][]tagging.Reference{"mapping": live}}
	service := NewQueries(repo, tags)
	values, err := service.Collections(t.Context(), model.CollectionQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tags.ids, []string{"mapping", "deleted"}) {
		t.Fatalf("queried tags: %v", tags.ids)
	}
	if !reflect.DeepEqual(
		values[0].TagSnapshot,
		live,
	) || !reflect.DeepEqual(
		values[1].TagSnapshot,
		frozen,
	) || values[2].TagSnapshot == nil || len(
		values[2].TagSnapshot,
	) != 0 {
		t.Fatalf("tags: %#v", values)
	}
	if !reflect.DeepEqual(repo.records[0].TagSnapshot, frozen) {
		t.Fatal("mutated repository snapshot")
	}
}

func TestCollectionTagFailureReturnsNoPartialProjection(t *testing.T) {
	t.Parallel()
	repo := &queryMemory{
		records: []model.CollectionRecord{{Collection: model.Collection{ID: "mapping"}, ImportState: "AWAITING_MAPPING"}},
	}
	tags := &tagMemory{err: errQueryTest}
	values, err := NewQueries(repo, tags).Collections(t.Context(), model.CollectionQuery{Limit: 10})
	if !errors.Is(err, errQueryTest) || values != nil {
		t.Fatalf("tag failure: %#v, %v", values, err)
	}
}

func TestFrozenCollectionsDoNotReadMutableTags(t *testing.T) {
	t.Parallel()
	tags := &tagMemory{err: errQueryTest}
	repo := &queryMemory{records: []model.CollectionRecord{{Collection: model.Collection{ID: "frozen"}, ImportState: "RUNNING"}}}
	if _, err := NewQueries(repo, tags).Collections(t.Context(), model.CollectionQuery{Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if tags.called != 0 {
		t.Fatal("frozen projection queried live tags")
	}
}
