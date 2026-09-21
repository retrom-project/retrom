package emulationstationimport

import (
	"encoding/json"
	"testing"
	"time"

	"retrom/internal/libraryimport"
)

func TestESExpiredRecoveryPreservesInterruptedReservedReview(t *testing.T) {
	for _, reason := range []string{"cancel", "timeout", "attempts"} {
		for _, checkpoint := range []string{"reserved", "attached", "metadata"} {
			t.Run(reason+"/"+checkpoint, func(t *testing.T) { assertRecoveredReview(t, reason, checkpoint) })
		}
	}
}

func assertRecoveredReview(t *testing.T, reason, checkpoint string) {
	t.Helper()
	fixture := newLifecycleFixture(t)
	started, unit := startLifecycleImport(t, fixture, "", "nes")
	item, imported := reserveExecutionReview(t, fixture, unit)
	metadata := prepareCancellationCheckpoint(t, fixture, unit, item, imported, checkpoint)
	if reason == "cancel" {
		requestExecutionCancellation(t, fixture, started.ID)
	}
	*fixture.now = fixture.now.Add(time.Minute)
	if reason == "timeout" {
		*fixture.now = time.UnixMilli(unit.DeadlineAtMS)
	}
	if reason == "attempts" {
		mustExecEmulationStationTest(t, fixture.database, `UPDATE jobs SET attempt_count=max_attempts WHERE id=?`, unit.JobID)
	}
	if err := fixture.service.recoverWork(fixture.context); err != nil {
		t.Fatal(err)
	}
	assertRecoveredReviewIdentity(t, fixture, unit, item, imported, metadata)
	before := executionReviewSnapshot(t, fixture, unit, imported.Items[0].ItemID)
	if err := fixture.service.recoverWork(fixture.context); err != nil {
		t.Fatal(err)
	}
	if after := executionReviewSnapshot(t, fixture, unit, imported.Items[0].ItemID); after != before {
		t.Fatal("recovery replay changed completed review")
	}
}

func assertRecoveredReviewIdentity(
	t *testing.T, fixture lifecycleFixture, unit work, item executionItem,
	imported libraryimport.ServerImportResult, metadata libraryimport.ServerMetadata,
) {
	t.Helper()
	var sourceState, linkedID, ordinaryState, draft string
	var ordinaryItems, games, reviewCount int64
	err := fixture.database.QueryRowContext(fixture.context, `SELECT source.execution_state,
COALESCE(source.library_import_item_id,''),ordinary.state,draft.metadata_json,
(SELECT count(*) FROM import_items),(SELECT count(*) FROM games),plan.review_pending_item_count
FROM emulationstation_import_items source JOIN emulationstation_imports plan ON plan.id=source.import_id,
import_items ordinary JOIN review_drafts draft ON draft.import_item_id=ordinary.id
WHERE source.id=? AND ordinary.id=?`, item.ID, imported.Items[0].ItemID).Scan(
		&sourceState, &linkedID, &ordinaryState, &draft, &ordinaryItems, &games, &reviewCount)
	if err != nil {
		t.Fatal(err)
	}
	var decoded libraryimport.ServerMetadata
	if err := json.Unmarshal([]byte(draft), &decoded); err != nil {
		t.Fatal(err)
	}
	if sourceState != "REVIEW_PENDING" || linkedID != imported.Items[0].ItemID || ordinaryState != "REVIEW_PENDING" ||
		decoded.Title != metadata.Title || ordinaryItems != 1 || games != 0 || reviewCount != 1 {
		t.Fatalf("recovered job=%s source=%s/%s ordinary=%s title=%s items=%d games=%d reviewCount=%d",
			unit.JobID, sourceState, linkedID, ordinaryState, decoded.Title, ordinaryItems, games, reviewCount)
	}
}
