package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	libraryimportmodel "retrom/internal/model/libraryimport"
	"retrom/internal/testkit/testsupport"

	"modernc.org/sqlite"
)

func queueDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db := metadataDatabase(t)
	metadataExec(t, db, `UPDATE review_drafts SET updated_at_ms=10 WHERE import_item_id='item'`)
	metadataExec(t, db, `UPDATE import_jobs SET total_item_count=3,review_pending_item_count=3 WHERE id='import'`)
	instance := testsupport.MustPlatformInstanceID(t, db, "gba/mgba")
	for _, entry := range []struct {
		id, title, digest string
		updated           int64
	}{
		{"item-2", "Second", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 10},
		{"item-3", "Third", "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", 20},
	} {
		metadataExec(t, db, `INSERT INTO import_items(id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,search_text,created_at_ms,updated_at_ms)
VALUES(?,'import',?,'REVIEW_PENDING','{"files":[{"logicalName":"game.gba"}]}',?,lower(?),1,1)`, entry.id, entry.digest, entry.digest, entry.title)
		metadataExec(t, db, `INSERT INTO review_drafts(id,import_item_id,target_platform_instance_id,metadata_json,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,json_object('title',?),1,1,?)`, entry.id, entry.id, instance, entry.title, entry.updated)
	}
	return db
}

func TestReviewQueueRepositoryUsesDraftClockAndIDForTies(t *testing.T) {
	t.Parallel()
	for _, sort := range []string{"UPDATED_ASC", "UPDATED_DESC"} {
		t.Run(sort, func(t *testing.T) {
			t.Parallel()
			db := queueDatabase(t)
			expected := []string{"item", "item-2", "item-3"}
			if sort == "UPDATED_DESC" {
				expected[0], expected[2] = expected[2], expected[0]
			}
			query := libraryimportmodel.ReviewQueueQuery{Filter: libraryimportmodel.ReviewQueueFilter{Sort: sort}, Limit: 1}
			for _, id := range expected {
				rows, err := NewReviewQueue(db).List(t.Context(), query)
				if err != nil || len(rows) != 1 || rows[0].ItemID != id {
					t.Fatalf("expected=%s rows=%#v err=%v", id, rows, err)
				}
				if rows[0].UpdatedAtMS != 10 && id != "item-3" {
					t.Fatalf("draft clock lost: %#v", rows[0])
				}
				query.After = &libraryimportmodel.ReviewQueuePosition{ItemID: id, UpdatedAtMS: rows[0].UpdatedAtMS}
			}
			rows, err := NewReviewQueue(db).List(t.Context(), query)
			if err != nil || rows == nil || len(rows) != 0 {
				t.Fatalf("after final row=%#v err=%v", rows, err)
			}
		})
	}
}

func TestReviewQueueRepositoryFiltersBySourceSearchPlatformAndBlocker(t *testing.T) {
	t.Parallel()
	db := queueDatabase(t)
	instance := testsupport.MustPlatformInstanceID(t, db, "gba/mgba")
	for _, tc := range []struct {
		name   string
		filter libraryimportmodel.ReviewQueueFilter
		ids    []string
	}{
		{"all", libraryimportmodel.ReviewQueueFilter{}, []string{"item", "item-2", "item-3"}},
		{"title", libraryimportmodel.ReviewQueueFilter{Query: "second"}, []string{"item-2"}},
		{"import", libraryimportmodel.ReviewQueueFilter{ImportJobID: "import"}, []string{"item", "item-2", "item-3"}},
		{"unknown import", libraryimportmodel.ReviewQueueFilter{ImportJobID: "missing"}, []string{}},
		{"unknown pegasus", libraryimportmodel.ReviewQueueFilter{PegasusImportID: "missing"}, []string{}},
		{"unknown es", libraryimportmodel.ReviewQueueFilter{EmulationStationImportID: "missing"}, []string{}},
		{"platform", libraryimportmodel.ReviewQueueFilter{PlatformInstanceID: instance}, []string{"item", "item-2", "item-3"}},
		{"unknown platform", libraryimportmodel.ReviewQueueFilter{PlatformInstanceID: "missing"}, []string{}},
		{"needs validation", libraryimportmodel.ReviewQueueFilter{BlockerCode: "NEEDS_VALIDATION"}, []string{"item", "item-2", "item-3"}},
		{"other blocker", libraryimportmodel.ReviewQueueFilter{BlockerCode: "DEPENDENCY_MISSING"}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := NewReviewQueue(db).List(t.Context(), libraryimportmodel.ReviewQueueQuery{Filter: tc.filter, Limit: 21})
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]string, 0, len(rows))
			for _, row := range rows {
				ids = append(ids, row.ItemID)
			}
			if !reflect.DeepEqual(ids, tc.ids) {
				t.Fatalf("ids=%#v want=%#v", ids, tc.ids)
			}
		})
	}
}

func TestReviewQueueRepositoryIncludesActiveTagNamesInSearch(t *testing.T) {
	t.Parallel()
	db := queueDatabase(t)
	const tag = "019b0000-0000-7000-8000-000000000021"
	metadataExec(t, db, `INSERT INTO tags(id,name,name_key,search_text,status,created_by_user_id,updated_by_user_id,created_at_ms,updated_at_ms)
VALUES(?,'Favorite Fixture','favorite fixture','favorite fixture','ACTIVE','actor','actor',1,1)`, tag)
	metadataExec(t, db, `INSERT INTO review_draft_tags(review_draft_id,tag_id,assigned_by_user_id,created_at_ms) VALUES('draft',?,'actor',1)`, tag)
	for _, filter := range []libraryimportmodel.ReviewQueueFilter{{Query: "favorite"}, {TagID: tag}} {
		rows, err := NewReviewQueue(db).List(t.Context(), libraryimportmodel.ReviewQueueQuery{Filter: filter, Limit: 21})
		if err != nil || len(rows) != 1 || rows[0].ItemID != "item" {
			t.Fatalf("tag filter rows=%#v err=%v", rows, err)
		}
	}
}

func TestReviewQueueRepositoryPreservesReadAndCancellationErrors(t *testing.T) {
	t.Parallel()
	db := queueDatabase(t)
	metadataExec(t, db, `UPDATE review_drafts SET metadata_json='{' WHERE import_item_id='item'`)
	rows, err := NewReviewQueue(db).List(t.Context(), libraryimportmodel.ReviewQueueQuery{Limit: 21})
	var cause *sqlite.Error
	if !errors.As(err, &cause) || rows != nil {
		t.Fatalf("corrupt metadata read error lost: %#v %v", rows, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	rows, err = NewReviewQueue(db).List(ctx, libraryimportmodel.ReviewQueueQuery{Limit: 21})
	if !errors.Is(err, context.Canceled) || rows != nil {
		t.Fatalf("canceled read error lost: %#v %v", rows, err)
	}
}
