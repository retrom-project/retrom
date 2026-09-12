//go:build integration

package libraryimport

import "testing"

func TestApproveKeepsUnfinishedImportAggregate(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ sibling, want string }{
		{"FAILED_RETRYABLE", "PARTIAL_FAILURE"},
		{"FAILED_FINAL", "PARTIAL_FAILURE"},
		{"HASHING", "RUNNING"},
		{"QUEUED", "RUNNING"},
		{"REVIEW_PENDING", "REVIEW_PENDING"},
	} {
		t.Run(test.sibling, func(t *testing.T) {
			t.Parallel()
			verifyApproveUnfinishedAggregate(t, test.sibling, test.want)
		})
	}
}

func verifyApproveUnfinishedAggregate(t *testing.T, sibling, want string) {
	t.Helper()
	fixture := newDeduplicateFixture(t)
	created := fixture.create(t, "Mixed approval", "Retrom owned approval aggregate", 2)
	pending, queued, running, failed := 1, 0, 0, 0
	switch sibling {
	case "QUEUED":
		queued = 1
	case "HASHING":
		running = 1
	case "FAILED_RETRYABLE", "FAILED_FINAL":
		failed = 1
	default:
		pending = 2
	}
	fixture.execute(t, `UPDATE import_items SET state=?,
 failed_stage=CASE WHEN ? IN ('FAILED_RETRYABLE','FAILED_FINAL') THEN 'HASHING' ELSE NULL END,
 last_error_code=CASE WHEN ? IN ('FAILED_RETRYABLE','FAILED_FINAL') THEN 'HASH_FAILED' ELSE NULL END
 WHERE id=?`, sibling, sibling, sibling, created.Items[1].ItemID)
	fixture.execute(t, `UPDATE import_jobs SET state=?,queued_item_count=?,running_item_count=?,
 review_pending_item_count=?,failed_item_count=?,completed_at_ms=NULL WHERE id=?`, want, queued, running, pending, failed, created.Created.ImportJobID)
	result, err := fixture.service.Approve(t.Context(), created.Items[0].ItemID, 1)
	if err != nil || result.Status != "PUBLISHED" {
		t.Fatalf("approve=%+v err=%v", result, err)
	}
	assertApprovedAggregate(t, fixture, created.Created.ImportJobID, want, pending-1)
}

func assertApprovedAggregate(t *testing.T, fixture deduplicateFixture, importID, want string, pending int) {
	t.Helper()
	var state, payload string
	var completed *int64
	var gotPending, published int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT state,completed_at_ms,
 review_pending_item_count,published_item_count,payload_state FROM import_jobs WHERE id=?`, importID).
		Scan(&state, &completed, &gotPending, &published, &payload); err != nil {
		t.Fatal(err)
	}
	if state != want || completed != nil || payload != "RETAINED" || gotPending != pending || published != 1 {
		t.Fatalf("approval misprojected unfinished task: state=%s completed=%v payload=%s pending=%d published=%d want=%s", state, completed, payload, gotPending, published, want)
	}
}
