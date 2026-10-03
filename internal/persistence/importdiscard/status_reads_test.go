package importdiscard_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"retrom/internal/service/importdiscard"
)

func TestDiscardPageStatusesMatchSingleReadsWithBoundedQueries(t *testing.T) {
	f := newFixture(t)
	ids := make([]string, 20)
	for index := range ids {
		batch := f.create(t, f.file(t, fmt.Sprintf("game-%d.nes", index), byte(index+1)))
		ids[index] = batch.Created.ImportJobID
	}
	for _, id := range ids[:3] {
		if _, err := f.service.Request(f.ctx, "IMPORT", id, adminID); err != nil {
			t.Fatal(err)
		}
	}
	f.exec(t, `UPDATE import_batch_discards SET state='FAILED',error_code=? WHERE import_id=?`,
		"IMPORT_BATCH_DISCARD_RELEASE_FAILED", ids[1])
	f.finish(t, "IMPORT", ids[2])
	// Some input is already disposed, while another batch is still awaiting review.
	for _, size := range []int{0, 1, 20} {
		before := f.db.Stats()
		got, err := f.service.GetMany(f.ctx, "IMPORT", ids[:size])
		after := f.db.Stats()
		if err != nil || len(got) != size {
			t.Fatalf("page size %d: got %d statuses: %v", size, len(got), err)
		}
		queries := int64(2)
		if size == 0 {
			queries = 0
		}
		if delta := after.SQLCalls - before.SQLCalls; delta != queries {
			t.Fatalf("page size %d used %d SQL calls, want %d", size, delta, queries)
		}
		for _, id := range ids[:size] {
			want, err := f.service.Get(f.ctx, "IMPORT", id)
			if err != nil || !reflect.DeepEqual(got[id], want) {
				t.Fatalf("status differs for %s: batch=%+v single=%+v error=%v", id, got[id], want, err)
			}
		}
	}
}

func TestDiscardSourcePageStatusPreservesAvailabilityAndDisposition(t *testing.T) {
	f := newFixture(t)
	first, _ := f.source(t, "SOURCE", f.file(t, "first.txt", 51))
	second, _ := f.source(t, "SOURCE", f.file(t, "second.txt", 52))
	if _, err := f.service.Request(f.ctx, "SOURCE", second, adminID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.GetMany(f.ctx, "SOURCE", []string{first, second})
	if err != nil || got[first].State != "AVAILABLE" || got[second].State != "REQUESTED" {
		t.Fatalf("source statuses: %+v, %v", got, err)
	}
	f.exec(t, `UPDATE source_imports SET import_job_id=NULL WHERE id=?`, first)
	got, err = f.service.GetMany(f.ctx, "SOURCE", []string{first})
	if err != nil || got[first].State != "UNAVAILABLE" {
		t.Fatalf("unstarted source status: %+v, %v", got, err)
	}
}

func TestDiscardPageRejectsMissingOrInvalidBatch(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct {
		kind string
		ids  []string
		want error
	}{
		{"IMPORT", []string{"bad"}, importdiscard.ErrInvalid},
		{"OTHER", []string{adminID}, importdiscard.ErrInvalid},
		{"IMPORT", []string{adminID}, importdiscard.ErrNotFound},
		{"IMPORT", make([]string, 21), importdiscard.ErrInvalid},
	} {
		_, err := f.service.GetMany(f.ctx, tc.kind, tc.ids)
		if !errors.Is(err, tc.want) {
			t.Fatalf("kind %s, ids %v: %v", tc.kind, tc.ids, err)
		}
	}
}
