//go:build integration

package libraryimport

import (
	"errors"
	"reflect"
	"testing"

	"retrom/internal/authn"
)

type discardOwnerSnapshot struct {
	State, PayloadState, ParentState                                 string
	Version, UpdatedAt, ParentVersion, Pending, Discarded, Published int64
	PayloadJob                                                       *string
}

func captureDiscardOwner(t *testing.T, fixture deduplicateFixture, sourceID string) discardOwnerSnapshot {
	t.Helper()
	var result discardOwnerSnapshot
	if err := fixture.database.QueryRowContext(t.Context(), `
SELECT i.execution_state,i.payload_state,i.version,i.updated_at_ms,i.payload_release_job_id,
p.state,p.version,p.review_pending_item_count,p.review_discarded_item_count,p.published_item_count
FROM source_import_items i JOIN source_imports p ON p.id=i.import_id WHERE i.id=?`, sourceID).
		Scan(&result.State, &result.PayloadState, &result.Version, &result.UpdatedAt, &result.PayloadJob, &result.ParentState, &result.ParentVersion, &result.Pending, &result.Discarded, &result.Published); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestDiscardLateFailureRollsBackLibraryOwnerAndPayload(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"owner aggregate", "payload event"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			testDiscardLateFailure(t, stage)
		})
	}
}

func testDiscardLateFailure(t *testing.T, stage string) {
	t.Helper()
	fixture, request := ownedSourceFixture(t)
	created, err := fixture.service.CreateOwnedServerSource(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	itemID := created.Items[0].ItemID
	fixture.execute(t, `UPDATE source_import_items SET execution_state='REVIEW_PENDING',completed_at_ms=? WHERE id=?`, ownedSourceNow().UnixMilli(), request.Intent.ItemID)
	fixture.execute(t, `UPDATE source_imports SET review_pending_item_count=1 WHERE id=?`, request.Intent.ImportID)
	before := captureDeduplicatePage(t, fixture, created.Created.ImportJobID)
	ownerBefore := captureDiscardOwner(t, fixture, request.Intent.ItemID)
	fault := newReviewDiscardFault(t, fixture, itemID, stage)
	ctx := authn.WithPrincipal(t.Context(), authn.Principal{UserID: "owner-actor"})
	result, err := fixture.service.Discard(ctx, itemID, 1, "Requested discard")
	if !errors.Is(err, fault.cause) || result != (DecisionResult{}) || fault.itemWrites != 1 || fault.faults != 1 {
		t.Fatalf("late failure cause/atomic boundary missing: result=%+v err=%v writes=%d fault.faults=%d", result, err, fault.itemWrites, fault.faults)
	}
	if fault.sourceWrites != 1 {
		t.Fatalf("late fault did not follow actual source write: %d", fault.sourceWrites)
	}
	assertDiscardRollback(t, fixture, created.Created.ImportJobID, itemID, request.Intent.ItemID, before, ownerBefore)
	fixture.service.database = fixture.database
	result, err = fixture.service.Discard(ctx, itemID, 1, "Requested discard")
	if err != nil || result.Status != "DISCARDED" || result.ItemID == "" {
		t.Fatalf("retry=%+v err=%v", result, err)
	}
	assertDiscardOwnerRetry(t, fixture, itemID, request.Intent.ItemID)
}

func assertDiscardOwnerRetry(t *testing.T, fixture deduplicateFixture, itemID, sourceID string) {
	t.Helper()
	owner := captureDiscardOwner(t, fixture, sourceID)
	if owner.State != "REVIEW_DISCARDED" || owner.Pending != 0 || owner.Discarded != 1 || owner.Published != 0 || owner.PayloadState != "RELEASING" || owner.PayloadJob == nil {
		t.Fatalf("discard owner was not finalized once: %+v", owner)
	}
	var state string
	if err := fixture.database.QueryRowContext(t.Context(), "SELECT state FROM import_items WHERE id=?", itemID).Scan(&state); err != nil || state != "DISCARDED" {
		t.Fatalf("discard state=%s err=%v", state, err)
	}
}

func TestDiscardBatchKeepsPerItemTransactions(t *testing.T) {
	t.Parallel()
	fixture := newDeduplicateFixture(t)
	created := fixture.create(t, "Batch discard", "Retrom owned batch discard fixture", 2)
	fault := newDeduplicateDiscardFault(t, fixture, created)
	done, err := fixture.service.DiscardBatchReviews(t.Context(), created.Created.ImportJobID)
	if !errors.Is(err, errDeduplicateDiscard) || done {
		t.Fatalf("batch failure=%v done=%v", err, done)
	}
	fault.assertReached(t)
	assertDeduplicateItemState(t, fixture, fault.firstID, "DISCARDED")
	assertDeduplicateItemState(t, fixture, fault.lastID, "REVIEW_PENDING")
	fixture.service.database = fixture.database
	done, err = fixture.service.DiscardBatchReviews(t.Context(), created.Created.ImportJobID)
	if err != nil || !done {
		t.Fatalf("batch retry=%v done=%v", err, done)
	}
	assertDeduplicateItemState(t, fixture, fault.lastID, "DISCARDED")
	var count int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM import_items WHERE state='DISCARDED'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("batch retry duplicated decisions: %d", count)
	}
}

func assertDiscardRollback(t *testing.T, fixture deduplicateFixture, importID, itemID, sourceID string, before deduplicatePageSnapshot, ownerBefore discardOwnerSnapshot) {
	t.Helper()
	assertDeduplicatePageUnchanged(t, fixture, importID, before)
	if after := captureDiscardOwner(t, fixture, sourceID); !reflect.DeepEqual(ownerBefore, after) {
		t.Fatalf("owner changed on rollback: before=%+v after=%+v", ownerBefore, after)
	}
	var events int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM import_items WHERE id=? AND state='DISCARDED'`, itemID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Fatalf("rollback retained %d review events", events)
	}
}
