//go:build integration

package libraryimport

import (
	"testing"

	"retrom/internal/service/importprogress"
)

type discardAggregateCase struct {
	name      string
	siblings  []string
	wantState string
}

func TestDiscardKeepsUnfinishedImportAggregate(t *testing.T) {
	t.Parallel()
	for _, test := range []discardAggregateCase{
		{"retryable failure", []string{"FAILED_RETRYABLE"}, "PARTIAL_FAILURE"},
		{"final failure", []string{"FAILED_FINAL"}, "PARTIAL_FAILURE"},
		{"running pipeline", []string{"HASHING"}, "RUNNING"},
		{"queued pipeline", []string{"QUEUED"}, "RUNNING"},
		{"running takes priority over failure", []string{"HASHING", "FAILED_RETRYABLE"}, "RUNNING"},
		{"pending review remains", []string{"REVIEW_PENDING"}, "REVIEW_PENDING"},
	} {
		t.Run(test.name, func(t *testing.T) { t.Parallel(); verifyDiscardUnfinishedAggregate(t, test) })
	}
}

func verifyDiscardUnfinishedAggregate(t *testing.T, test discardAggregateCase) {
	t.Helper()
	fixture := newDeduplicateFixture(t)
	created := fixture.create(t, "Mixed discard", "Retrom owned mixed import aggregate", len(test.siblings)+1)
	pending, queued, running, failed := 1, 0, 0, 0
	for index, state := range test.siblings {
		switch state {
		case "QUEUED":
			queued++
		case "HASHING":
			running++
		case "FAILED_RETRYABLE", "FAILED_FINAL":
			failed++
		case "REVIEW_PENDING":
			pending++
		default:
			t.Fatalf("unhandled aggregate fixture state %s", state)
		}
		fixture.execute(t, `UPDATE import_items SET state=?,
 failed_stage=CASE WHEN ? IN ('FAILED_RETRYABLE','FAILED_FINAL') THEN 'HASHING' ELSE NULL END,
 last_error_code=CASE WHEN ? IN ('FAILED_RETRYABLE','FAILED_FINAL') THEN 'HASH_FAILED' ELSE NULL END
 WHERE id=?`, state, state, state, created.Items[index+1].ItemID)
	}
	fixture.execute(t, `UPDATE import_jobs SET state=?,queued_item_count=?,running_item_count=?,
 review_pending_item_count=?,failed_item_count=?,completed_at_ms=NULL WHERE id=?`, test.wantState, queued, running, pending, failed, created.Created.ImportJobID)
	result, err := fixture.service.Discard(t.Context(), created.Items[0].ItemID, 1, "")
	if err != nil || result.Status != "DISCARDED" {
		t.Fatalf("discard=%+v err=%v", result, err)
	}
	assertDiscardUnfinishedAggregate(t, fixture, created.Created.ImportJobID, test.wantState, importprogress.Counts{
		Queued: int64(queued), Running: int64(running), ReviewPending: int64(pending - 1), Failed: int64(failed),
	})
}

func assertDiscardUnfinishedAggregate(
	t *testing.T, fixture deduplicateFixture, importID, wantState string, expected importprogress.Counts,
) {
	t.Helper()
	var state, payloadState string
	var completed *int64
	var gotQueued, gotRunning, gotPending, gotFailed, discarded int64
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT state,completed_at_ms,queued_item_count,running_item_count,
 review_pending_item_count,failed_item_count,discarded_item_count,payload_state FROM import_jobs WHERE id=?`, importID).
		Scan(&state, &completed, &gotQueued, &gotRunning, &gotPending, &gotFailed, &discarded, &payloadState); err != nil {
		t.Fatal(err)
	}
	if state != wantState || completed != nil || payloadState != "RETAINED" {
		t.Fatalf("unfinished import prematurely completed: state=%s completed=%v payload=%s; want=%s", state, completed, payloadState, wantState)
	}
	if gotQueued != expected.Queued || gotRunning != expected.Running || gotPending != expected.ReviewPending || gotFailed != expected.Failed || discarded != 1 {
		t.Fatalf("discard counters changed unrelated siblings: queued=%d running=%d pending=%d failed=%d discarded=%d", gotQueued, gotRunning, gotPending, gotFailed, discarded)
	}
}
