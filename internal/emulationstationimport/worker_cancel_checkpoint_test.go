package emulationstationimport

import "testing"

func TestESItemCancellationCheckpointPreservesReservedReview(t *testing.T) {
	fixture := newLifecycleFixture(t)
	started, unit := startLifecycleImport(t, fixture, "", "nes")
	item, imported := reserveExecutionReview(t, fixture, unit)
	metadata := prepareCancellationCheckpoint(t, fixture, unit, item, imported, "reserved")
	requestExecutionCancellation(t, fixture, started.ID)
	fixture.service.processItem(fixture.context, unit, fixture.service.roots[unit.RootID], item)
	closed, err := fixture.service.closeCancelled(fixture.context, unit)
	if err != nil || !closed {
		t.Fatalf("closed=%v error=%v", closed, err)
	}
	assertRecoveredReviewIdentity(t, fixture, unit, item, imported, metadata)
}
