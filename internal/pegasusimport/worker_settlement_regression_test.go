package pegasusimport

import (
	"fmt"
	"testing"
)

func TestWorkerFailureCannotCloseReplacedExecution(t *testing.T) {
	t.Parallel()
	service, unit, _ := handoffFixture(t)
	mustExecPegasusTest(t.Context(), t, service.database, `UPDATE jobs SET worker_id='replacement' WHERE id='work'`)
	service.fail(t.Context(), unit, "INTERNAL_ERROR", true)
	var state string
	if err := service.database.QueryRowContext(t.Context(), `SELECT state FROM pegasus_imports WHERE id='import'`).Scan(
		&state,
	); err != nil {
		t.Fatal(err)
	}
	if state != "RUNNING" {
		t.Fatalf("old worker closed parent=%s", state)
	}
}

func TestWorkerFailureRollsBackWhenEventCannotBeWritten(t *testing.T) {
	t.Parallel()
	service, unit, _ := handoffFixture(t)
	mustExecPegasusTest(t.Context(), t, service.database, `DROP TABLE job_events`)
	service.fail(t.Context(), unit, "INTERNAL_ERROR", true)
	var state string
	if err := service.database.QueryRowContext(t.Context(), `SELECT state FROM pegasus_imports WHERE id='import'`).Scan(
		&state,
	); err != nil {
		t.Fatal(err)
	}
	if state != "RUNNING" {
		t.Fatalf("failure without event committed parent=%s", state)
	}
	assertHandoffDraftUntouched(t, service)
}

func TestWorkerCancellationCannotCloseReplacedExecution(t *testing.T) {
	t.Parallel()
	service, unit, _ := handoffFixture(t)
	mustExecPegasusTest(
		t.Context(),
		t,
		service.database,
		`UPDATE jobs SET state='CANCEL_REQUESTED',worker_id='replacement',cancel_reason='stop',cancel_requested_at_ms=10 WHERE id='work';UPDATE pegasus_imports SET state='CANCEL_REQUESTED',cancel_reason='stop' WHERE id='import'`,
	)
	closed, err := service.closeCancelled(t.Context(), unit)
	if err == nil || closed {
		t.Fatalf("old worker cancelled replacement: closed=%v err=%v", closed, err)
	}
}

func TestWorkerSettlementRetainsBoundReviewAndClosesOnlyUnfinishedSource(t *testing.T) {
	t.Parallel()
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", cancel), func(t *testing.T) {
			service, unit, _ := handoffFixture(t)
			mustExecPegasusTest(t.Context(), t, service.database, `UPDATE pegasus_imports SET game_count=2 WHERE id='import';
INSERT INTO pegasus_import_items(id,import_id,metadata_relative_path,game_ordinal,source_key,title,
discovery_state,execution_state,metadata_json,source_manifest_json,source_manifest_digest,created_at_ms,updated_at_ms)
VALUES('unfinished','import','metadata.pegasus.txt',1,'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
'Unfinished','READY','COPYING','{}','{}','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',1,1);`)
			if cancel {
				mustExecPegasusTest(
					t.Context(),
					t,
					service.database,
					`UPDATE jobs SET state='CANCEL_REQUESTED',cancel_reason='stop',cancel_requested_at_ms=10 WHERE id='work';
UPDATE pegasus_imports SET state='CANCEL_REQUESTED',cancel_reason='stop' WHERE id='import'`,
				)
				if closed, err := service.closeCancelled(t.Context(), unit); !closed || err != nil {
					t.Fatalf("cancel=%v %v", closed, err)
				}
			} else {
				service.fail(t.Context(), unit, "INTERNAL_ERROR", true)
			}
			assertSettledReview(t, service, cancel)
		})
	}
}

type settledReviewOutcome struct {
	Parent, Job, Review, Item, Payload string
	Pending, Failed, Cancelled         int
	Retryable                          bool
}

func assertSettledReview(t *testing.T, service *Service, cancel bool) {
	t.Helper()
	var actual settledReviewOutcome
	err := service.database.QueryRowContext(t.Context(), `SELECT plan.state,job.state,plan.review_pending_item_count,
plan.failed_item_count,plan.cancelled_item_count,item.execution_state,item.payload_state,unfinished.execution_state,
unfinished.retryable FROM pegasus_imports plan JOIN jobs job ON job.id=plan.import_job_id
JOIN pegasus_import_items item ON item.id='item' JOIN pegasus_import_items unfinished ON unfinished.id='unfinished'
WHERE plan.id='import'`).Scan(&actual.Parent, &actual.Job, &actual.Pending, &actual.Failed, &actual.Cancelled,
		&actual.Review, &actual.Payload, &actual.Item, &actual.Retryable)
	if err != nil {
		t.Fatal(err)
	}
	expected := settledReviewOutcome{
		Parent:    "FAILED",
		Job:       "FAILED",
		Review:    "REVIEW_PENDING",
		Item:      "COMMIT_FAILED",
		Payload:   "RETAINED",
		Pending:   1,
		Failed:    1,
		Retryable: true,
	}
	if cancel {
		expected.Parent, expected.Job, expected.Item = "CANCELLED", "CANCELLED", "CANCELLED"
		expected.Failed, expected.Cancelled, expected.Retryable = 0, 1, false
	}
	if actual != expected {
		t.Fatalf("settlement=%#v want=%#v", actual, expected)
	}
	var title string
	if err := service.database.QueryRowContext(t.Context(), `SELECT json_extract(metadata_json,'$.title') FROM review_drafts WHERE id='handoff-draft'`).Scan(
		&title,
	); err != nil {
		t.Fatal(
			err,
		)
	}
	if title != "Changed" {
		t.Fatalf("bound review was not reconciled: %s", title)
	}
}
