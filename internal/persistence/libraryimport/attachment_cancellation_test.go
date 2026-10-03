package libraryimport

import (
	"testing"
	"time"

	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

func TestAttachmentCancellationIsAtomicBeforeObservation(t *testing.T) {
	for _, kind := range []string{"REVIEW_ARCADE_PARENT_VALIDATE", "REVIEW_MULTI_DISC_VALIDATE"} {
		for _, state := range []string{"QUEUED", "RUNNING", "FAILED"} {
			t.Run(kind+"/"+state, func(t *testing.T) {
				testAttachmentCancellation(t, kind, state)
			})
		}
	}
}

func testAttachmentCancellation(t *testing.T, kind, state string) {
	t.Helper()
	initial := state
	if state == "FAILED" {
		initial = "RUNNING"
	}
	db := attachmentExecutionFixture(t, kind, initial)
	if state == "FAILED" {
		metadataExec(t, db, `UPDATE jobs SET state='FAILED',error_code='UNAVAILABLE',
 error_retryable=1,finished_at_ms=3 WHERE id='parent-job'`)
	}
	service := libraryservice.NewAttachmentExecutions(NewAttachmentExecutions(db), func() time.Time { return time.UnixMilli(5) })
	result, err := service.CancelJob(t.Context(), libraryservice.ImportJobCancellation{
		JobID: "parent-job", ImportID: "item", Reason: "cancel before reading",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "CANCELLED"
	if state == "RUNNING" {
		want = "CANCEL_REQUESTED"
	}
	if result.State != want || result.Pending != (state == "RUNNING") {
		t.Fatalf("result=%+v", result)
	}
	assertAttachmentCancellationState(t, db, state)

	if repeated, err := service.CancelJob(t.Context(), libraryservice.ImportJobCancellation{
		JobID: "parent-job", ImportID: "item", Reason: "repeat request",
	}); err != nil || repeated != result {
		t.Fatalf("repeat cancellation changed result: %+v %v", repeated, err)
	}
}

func assertAttachmentCancellationState(t *testing.T, db dbapi.DB, state string) {
	t.Helper()
	var attachment string
	var lease *int64
	var worker string
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT attachment.state,job.leased_until_ms,COALESCE(job.worker_id,'')
 FROM jobs job JOIN (
 SELECT job_id,state FROM review_arcade_parent_attachments UNION ALL SELECT job_id,state FROM review_multidisc_attachments
 ) attachment ON attachment.job_id=job.id WHERE job.id='parent-job'`).Scan(&attachment, &lease, &worker); err != nil {
		t.Fatal(err)
	}
	if state == "RUNNING" {
		if attachment != "PENDING" || lease == nil || worker != "worker" {
			t.Fatalf("live worker lost fence: %s %v %s", attachment, lease, worker)
		}
	} else if attachment != "CANCELLED" || worker != "" || lease != nil {
		t.Fatalf("partial cancellation: %s %v %s", attachment, lease, worker)
	}
}

func TestFinishedAttachmentSchedulesOnlyItsOwnInputRelease(t *testing.T) {
	db := attachmentExecutionFixture(t, "REVIEW_ARCADE_PARENT_VALIDATE", "QUEUED")
	metadataExec(t, db, `INSERT INTO upload_consumptions(id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms)
 VALUES('input-one','upload','parent-upload','REVIEW_ARCADE_PARENT','attachment',1),
 ('input-two','upload','parent-upload','REVIEW_ARCADE_PARENT','other-attachment',1)`)
	service := libraryservice.NewAttachmentExecutions(NewAttachmentExecutions(db), func() time.Time { return time.UnixMilli(5) })
	if _, err := service.CancelJob(t.Context(), libraryservice.ImportJobCancellation{
		JobID: "parent-job", ImportID: "item", Reason: "cancel",
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	var own, other int
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT
 (SELECT count(*) FROM jobs WHERE scope_type='UPLOAD_CONSUMPTION' AND scope_id='input-one'),
 (SELECT count(*) FROM jobs WHERE scope_type='UPLOAD_CONSUMPTION' AND scope_id='input-two')`).Scan(&own, &other); err != nil {
		t.Fatal(err)
	}
	if own != 1 || other != 0 {
		t.Fatalf("release scope: own=%d other=%d", own, other)
	}
}
