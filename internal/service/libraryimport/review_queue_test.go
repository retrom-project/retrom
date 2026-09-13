package libraryimport

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"retrom/internal/service/tagging"
)

type queueMemory struct {
	records []ReviewQueueRecord
	query   ReviewQueueQuery
	err     error
	calls   int
}

func (r *queueMemory) List(_ context.Context, query ReviewQueueQuery) ([]ReviewQueueRecord, error) {
	r.query = query
	r.calls++
	return r.records, r.err
}

type queueTagMemory struct {
	ids    []string
	values map[string][]tagging.Reference
	err    error
	calls  int
}

func (r *queueTagMemory) ReviewReferences(_ context.Context, ids []string) (map[string][]tagging.Reference, error) {
	r.ids = append([]string{}, ids...)
	r.calls++
	return r.values, r.err
}
func queueString(value string) *string { return &value }

func TestReviewQueueNormalizesFilters(t *testing.T) {
	t.Parallel()
	value, err := NormalizeReviewQueueFilter(ReviewQueueFilter{Query: "  Hello\t界  World \n", TagID: "019b0000-0000-7000-8000-000000000001"})
	if err != nil || value.Query != "hello 界 world" || value.Sort != "UPDATED_ASC" || value.Limit != 20 {
		t.Fatalf("normalized=%#v err=%v", value, err)
	}
}

func TestReviewQueueRejectsInvalidFiltersBeforeReading(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		filter ReviewQueueFilter
		after  *ReviewQueuePosition
	}{
		{name: "query length", filter: ReviewQueueFilter{Query: strings.Repeat("界", 201)}},
		{name: "tag id", filter: ReviewQueueFilter{TagID: "wrong"}},
		{name: "sort", filter: ReviewQueueFilter{Sort: "TITLE_ASC"}},
		{name: "negative limit", filter: ReviewQueueFilter{Limit: -1}},
		{name: "large limit", filter: ReviewQueueFilter{Limit: 21}},
		{name: "ordinary pegasus", filter: ReviewQueueFilter{ImportJobID: "job", PegasusImportID: "pegasus"}},
		{name: "ordinary es", filter: ReviewQueueFilter{ImportJobID: "job", EmulationStationImportID: "es"}},
		{name: "two server sources", filter: ReviewQueueFilter{PegasusImportID: "pegasus", EmulationStationImportID: "es"}},
		{name: "missing cursor id", after: &ReviewQueuePosition{UpdatedAtMS: 1}},
		{name: "negative cursor time", after: &ReviewQueuePosition{ItemID: "item", UpdatedAtMS: -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := &queueMemory{}
			tags := &queueTagMemory{}
			_, err := NewReviewQueue(repo, tags).List(t.Context(), tc.filter, tc.after)
			if !errors.Is(err, ErrReviewQuery) || repo.calls != 0 || tags.calls != 0 {
				t.Fatalf("invalid filter reached storage: %v repo=%d tags=%d", err, repo.calls, tags.calls)
			}
		})
	}
}

func TestReviewQueueUsesLookaheadAndLoadsTagsForVisibleItems(t *testing.T) {
	t.Parallel()
	repo := &queueMemory{records: []ReviewQueueRecord{{ItemID: "first", UpdatedAtMS: 10}, {ItemID: "second", UpdatedAtMS: 20}, {ItemID: "lookahead", UpdatedAtMS: 30}}}
	references := []tagging.Reference{{TagID: "tag", Name: "Tag"}}
	tags := &queueTagMemory{values: map[string][]tagging.Reference{"first": references}}
	after := &ReviewQueuePosition{ItemID: "previous", UpdatedAtMS: 9}
	page, err := NewReviewQueue(repo, tags).List(t.Context(), ReviewQueueFilter{Query: " Q ", Limit: 2, Sort: "UPDATED_DESC"}, after)
	if err != nil || len(page.Items) != 2 || page.Next == nil || *page.Next != (ReviewQueuePosition{ItemID: "second", UpdatedAtMS: 20}) {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	if repo.query.Limit != 3 || repo.query.Filter.Query != "q" || repo.query.Filter.Sort != "UPDATED_DESC" || repo.query.After != after {
		t.Fatalf("query=%#v", repo.query)
	}
	if !reflect.DeepEqual(tags.ids, []string{"first", "second"}) || !reflect.DeepEqual(page.Items[0].Tags, references) || page.Items[1].Tags == nil {
		t.Fatalf("tag projection=%#v ids=%#v", page.Items, tags.ids)
	}
}

func TestReviewQueueProjectsValidationAndSourceCoverPriority(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		record       ReviewQueueRecord
		kind, status string
		cover        *string
		blockers     []string
	}{
		{name: "ordinary missing validation", kind: "STANDARD", status: "NEEDS_VALIDATION", blockers: []string{}},
		{name: "ready", record: ReviewQueueRecord{ValidationStatus: queueString("READY"), CompatibilityCode: queueString("OK")}, kind: "STANDARD", status: "READY", blockers: []string{}},
		{name: "blocked", record: ReviewQueueRecord{ValidationStatus: queueString("BLOCKED"), CompatibilityCode: queueString("DEPENDENCY_MISSING")}, kind: "STANDARD", status: "BLOCKED", blockers: []string{"DEPENDENCY_MISSING"}},
		{name: "pegasus", record: ReviewQueueRecord{Pegasus: &ReviewQueueSource{ItemID: "peg-item", ImportID: "peg-import", Label: queueString("Collection"), HasCover: true}}, kind: "PEGASUS", status: "NEEDS_VALIDATION", cover: queueString("/api/v1/admin/review-assets/peg-item?kind=COVER"), blockers: []string{}},
		{name: "es", record: ReviewQueueRecord{EmulationStation: &ReviewQueueSource{ItemID: "es-item", ImportID: "es-import", HasCover: true}}, kind: "EMULATIONSTATION", status: "NEEDS_VALIDATION", cover: queueString("/api/v1/admin/review-assets/es-item?kind=COVER"), blockers: []string{}},
		{name: "selected cover first", record: ReviewQueueRecord{CoverAssetID: queueString("selected"), Pegasus: &ReviewQueueSource{ItemID: "peg-item", ImportID: "peg-import", HasCover: true}}, kind: "PEGASUS", status: "NEEDS_VALIDATION", cover: queueString("/api/v1/admin/review-assets/selected"), blockers: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.record.ItemID = "item"
			repo := &queueMemory{records: []ReviewQueueRecord{tc.record}}
			page, err := NewReviewQueue(repo, &queueTagMemory{}).List(t.Context(), ReviewQueueFilter{}, nil)
			if err != nil || len(page.Items) != 1 {
				t.Fatalf("page=%#v err=%v", page, err)
			}
			item := page.Items[0]
			if item.SourceKind != tc.kind || item.ValidationStatus != tc.status || !reflect.DeepEqual(item.CoverURL, tc.cover) || !reflect.DeepEqual(item.BlockerCodes, tc.blockers) || item.ValidationJobID != nil {
				t.Fatalf("projection=%#v", item)
			}
		})
	}
}

func TestReviewQueuePreservesStorageAndTagFailures(t *testing.T) {
	t.Parallel()
	cause := errors.New("queue storage unavailable")
	for _, phase := range []string{"rows", "tags"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			repo := &queueMemory{records: []ReviewQueueRecord{{ItemID: "item"}}}
			tags := &queueTagMemory{}
			if phase == "rows" {
				repo.err = cause
			} else {
				tags.err = cause
			}
			page, err := NewReviewQueue(repo, tags).List(t.Context(), ReviewQueueFilter{}, nil)
			if !errors.Is(err, cause) || page.Items != nil || page.Next != nil {
				t.Fatalf("failure returned partial page: %#v %v", page, err)
			}
		})
	}
}

func TestReviewQueueEmptyResultHasNonNullItemsAndNoCursor(t *testing.T) {
	t.Parallel()
	tags := &queueTagMemory{}
	page, err := NewReviewQueue(&queueMemory{}, tags).List(t.Context(), ReviewQueueFilter{}, nil)
	if err != nil || page.Items == nil || len(page.Items) != 0 || page.Next != nil || tags.calls != 0 {
		t.Fatalf("empty page=%#v err=%v tags=%d", page, err, tags.calls)
	}
}
