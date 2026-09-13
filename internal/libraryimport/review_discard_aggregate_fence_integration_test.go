//go:build integration

package libraryimport

import (
	"errors"
	"testing"

	"retrom/internal/dbexec"
	repository "retrom/internal/persistence/libraryimport"
	"retrom/internal/service/importprogress"
	application "retrom/internal/service/libraryimport"
)

func TestDiscardWriterRequiresCurrentParentAggregate(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, mutation string }{
		{"parent version", "version=version+1"},
		{"pending count", "review_pending_item_count=review_pending_item_count-1,discarded_item_count=discarded_item_count+1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			verifyDiscardParentFence(t, test.mutation)
		})
	}
}

func verifyDiscardParentFence(t *testing.T, mutation string) {
	t.Helper()
	fixture := newDeduplicateFixture(t)
	created := fixture.create(t, "Parent discard fence", "Retrom owned parent CAS", 2)
	before := captureDeduplicatePage(t, fixture, created.Created.ImportJobID)
	transaction, err := fixture.database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(transaction)
	scope := repository.BindReviewDiscard(transaction)
	snapshot, found, err := scope.Reader.Snapshot(t.Context(), created.Items[0].ItemID)
	if err != nil || !found {
		t.Fatalf("snapshot=%+v found=%v err=%v", snapshot, found, err)
	}
	if _, err := transaction.ExecContext(t.Context(), "UPDATE import_jobs SET "+mutation+" WHERE id=?", created.Created.ImportJobID); err != nil {
		t.Fatal(err)
	}
	err = scope.Writer.DiscardItem(t.Context(), application.ReviewDiscardChange{
		ItemID: created.Items[0].ItemID, ImportID: created.Created.ImportJobID, ExpectedVersion: 1,
		NowMS: fixture.service.now().UnixMilli(), Aggregate: application.ReviewDiscardAggregateChange{
			ExpectedVersion: snapshot.Aggregate.Version, ExpectedPending: 2,
			Projection: importprogress.Projection{State: "REVIEW_PENDING"},
		},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("discard ignored parent fence: %v", err)
	}
	var state string
	if err := transaction.QueryRowContext(t.Context(), "SELECT state FROM import_items WHERE id=?", created.Items[0].ItemID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "DISCARDED" {
		t.Fatalf("parent fence was not tested after real item write: %s", state)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertDeduplicatePageUnchanged(t, fixture, created.Created.ImportJobID, before)
}
