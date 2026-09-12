package emulationstationimport

import (
	"testing"
	"time"
)

func TestESSettlementCompletesRetryableSourceWithReservedReview(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		for _, attached := range []bool{false, true} {
			name := map[bool]string{false: "live", true: "recovery"}[recovery] + map[bool]string{false: "/reserved", true: "/attached"}[attached]
			t.Run(name, func(t *testing.T) { assertFailedReservedReviewSettlement(t, recovery, attached) })
		}
	}
}

func assertFailedReservedReviewSettlement(t *testing.T, recovery, attached bool) {
	t.Helper()
	fixture := newLifecycleFixture(t)
	started, unit := startLifecycleImport(t, fixture, "", "nes")
	item, imported := reserveExecutionReview(t, fixture, unit)
	checkpoint := "reserved"
	if attached {
		checkpoint = "attached"
	}
	metadata := prepareCancellationCheckpoint(t, fixture, unit, item, imported, checkpoint)
	fixture.service.closeItem(fixture.context, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "")
	var state string
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT execution_state FROM emulationstation_import_items WHERE id=?`, item.ID).Scan(&state); err != nil || state != "COMMIT_FAILED" {
		t.Fatalf("failure fixture state=%s error=%v", state, err)
	}
	requestExecutionCancellation(t, fixture, started.ID)
	if recovery {
		*fixture.now = fixture.now.Add(time.Minute)
		if err := fixture.service.recoverWork(fixture.context); err != nil {
			t.Fatal(err)
		}
	} else if closed, err := fixture.service.closeCancelled(fixture.context, unit); err != nil || !closed {
		t.Fatalf("closed=%v error=%v", closed, err)
	}
	assertRecoveredReviewIdentity(t, fixture, unit, item, imported, metadata)
}

func TestESLiveCancellationRetainsFailedOutcomeWithoutOfferingRetry(t *testing.T) {
	fixture := newLifecycleFixture(t)
	started, unit := startLifecycleImport(t, fixture, "", "nes")
	item, found, err := fixture.service.nextItem(fixture.context, unit.ImportID)
	if err != nil || !found {
		t.Fatalf("next=%v error=%v", found, err)
	}
	fixture.service.closeItem(fixture.context, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true, "")
	requestExecutionCancellation(t, fixture, started.ID)
	closed, err := fixture.service.closeCancelled(fixture.context, unit)
	if err != nil || !closed {
		t.Fatalf("closed=%v error=%v", closed, err)
	}
	var state string
	var retryable, failed int
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT state,retryable,failed_item_count FROM emulationstation_imports WHERE id=?`, unit.ImportID).Scan(&state, &retryable, &failed); err != nil {
		t.Fatal(err)
	}
	if state != "CANCELLED" || retryable != 0 || failed != 1 {
		t.Fatalf("state=%s retryable=%d failed=%d", state, retryable, failed)
	}
}
