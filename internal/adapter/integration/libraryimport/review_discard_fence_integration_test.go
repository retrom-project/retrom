//go:build integration

package libraryimport

import (
	"errors"
	"testing"

	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
	repository "retrom/internal/repo/libraryimport"
)

func TestDiscardWriterRequiresCurrentDraftVersion(t *testing.T) {
	t.Parallel()
	fixture := newDeduplicateFixture(t)
	created := fixture.create(t, "Discard stale writer", "Retrom owned discard CAS", 1)
	itemID := created.Items[0].ItemID
	transaction, err := fixture.database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(transaction)
	if _, err := transaction.ExecContext(t.Context(), `UPDATE review_drafts SET version=2 WHERE import_item_id=?`, itemID); err != nil {
		t.Fatal(err)
	}
	err = repository.BindReviewDiscard(transaction).Writer.DiscardItem(t.Context(), application.ReviewDiscardChange{
		ItemID: itemID, ImportID: created.Created.ImportJobID, ExpectedVersion: 1, NowMS: fixture.service.now().UnixMilli(),
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("writer ignored current draft version: %v", err)
	}
	var state string
	if err := transaction.QueryRowContext(t.Context(), `SELECT state FROM import_items WHERE id=?`, itemID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "REVIEW_PENDING" {
		t.Fatalf("stale writer changed state=%s", state)
	}
}

func TestDiscardOwnerWriterSeparatesSingleAndBatchAuthority(t *testing.T) {
	t.Parallel()
	for _, mode := range []application.ReviewDiscardMode{application.ReviewDiscardSingle, application.ReviewDiscardBatch} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			assertDiscardOwnerAuthority(t, mode)
		})
	}
}

func assertDiscardOwnerAuthority(t *testing.T, mode application.ReviewDiscardMode) {
	t.Helper()
	fixture, request := ownedSourceFixture(t)
	created, err := fixture.service.CreateOwnedServerSource(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	fixture.execute(t, `UPDATE pegasus_import_items SET execution_state='CANCELLED',completed_at_ms=? WHERE id=?`, ownedSourceNow().UnixMilli(), request.Intent.ItemID)
	transaction, err := fixture.database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(transaction)
	if _, err := transaction.ExecContext(t.Context(), `UPDATE import_items SET state='DISCARDED',completed_at_ms=? WHERE id=?`, ownedSourceNow().UnixMilli(), created.Items[0].ItemID); err != nil {
		t.Fatal(err)
	}
	err = repository.TransitionReviewOwners(t.Context(), transaction, application.ReviewOwnerTransition{
		ItemID: created.Items[0].ItemID, State: application.ReviewOwnerDiscarded, Mode: mode, NowMS: ownedSourceNow().UnixMilli(),
	})
	if mode == application.ReviewDiscardBatch {
		if err != nil {
			t.Fatalf("batch lost cancelled-source cleanup: %v", err)
		}
		return
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("single owner writer accepted unhanded state: %v", err)
	}
	var state string
	if err := transaction.QueryRowContext(t.Context(), `SELECT execution_state FROM pegasus_import_items WHERE id=?`, request.Intent.ItemID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "CANCELLED" {
		t.Fatalf("single owner writer changed source to %s", state)
	}
}
