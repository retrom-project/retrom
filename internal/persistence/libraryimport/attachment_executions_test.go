package libraryimport

import (
	"context"
	"errors"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

func attachmentExecutionFixture(t *testing.T, kind, state string) dbapi.DB {
	t.Helper()
	database := metadataDatabase(t)
	insertArcadeParentCommitTerminalFixture(t, database, kind, state)
	if kind == "REVIEW_MULTI_DISC_VALIDATE" {
		metadataExec(t, database, `DELETE FROM review_arcade_parent_attachments WHERE id='attachment'`)
		metadataExec(t, database, `INSERT INTO review_multidisc_attachments
(id,import_item_id,review_draft_id,requested_by_user_id,base_source_snapshot_id,upload_session_id,
expected_set_digest,state,diagnostics_json,job_id,created_at_ms,updated_at_ms)
VALUES('attachment','item','item','actor','snapshot','upload',lower(hex(randomblob(32))),
'PENDING','{}','parent-job',1,1)`)
	}
	metadataExec(t, database, `UPDATE jobs SET leased_until_ms=10,execution_deadline_at_ms=2000 WHERE id='parent-job'`)
	return database
}

func TestInterruptedAttachmentBecomesRunnable(t *testing.T) {
	for _, kind := range []string{"REVIEW_ARCADE_PARENT_VALIDATE", "REVIEW_MULTI_DISC_VALIDATE"} {
		t.Run(kind, func(t *testing.T) {
			database := attachmentExecutionFixture(t, kind, "RUNNING")
			now := int64(20)
			service := libraryservice.NewAttachmentExecutions(NewAttachmentExecutions(database), func() time.Time { return time.UnixMilli(now) })
			if err := service.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			if jobs, err := service.Queued(t.Context()); err != nil || len(jobs) != 0 {
				t.Fatalf("backoff jobs=%+v err=%v", jobs, err)
			}
			now = 1020
			jobs, err := service.Queued(t.Context())
			if err != nil || len(jobs) != 1 {
				t.Fatalf("expired worker stranded: jobs=%+v err=%v", jobs, err)
			}
			assertRecoveredAttachmentExecution(t, jobs[0])
			if err := service.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			var events int
			if err := dbapi.QueryRowContext(t.Context(), database, `SELECT count(*) FROM job_events WHERE job_id='parent-job'`).Scan(&events); err != nil {
				t.Fatal(err)
			}
			if events != 1 {
				t.Fatalf("duplicate recovery events=%d", events)
			}
		})
	}
}

func TestAttachmentRecoveryFinishesCancellationForBothKinds(t *testing.T) {
	for _, kind := range []string{"REVIEW_ARCADE_PARENT_VALIDATE", "REVIEW_MULTI_DISC_VALIDATE"} {
		t.Run(kind, func(t *testing.T) {
			database := attachmentExecutionFixture(t, kind, "CANCEL_REQUESTED")
			service := libraryservice.NewAttachmentExecutions(NewAttachmentExecutions(database), func() time.Time { return time.UnixMilli(20) })
			if err := service.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := service.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			var state, attachment string
			if err := dbapi.QueryRowContext(t.Context(), database, `SELECT job.state,attachment.state FROM jobs job JOIN (
SELECT job_id,state FROM review_arcade_parent_attachments UNION ALL SELECT job_id,state FROM review_multidisc_attachments
) attachment ON attachment.job_id=job.id WHERE job.id='parent-job'`).Scan(&state, &attachment); err != nil {
				t.Fatal(err)
			}
			if state != "CANCELLED" || attachment != "CANCELLED" {
				t.Fatalf("job=%s attachment=%s", state, attachment)
			}
			if jobs, err := service.Queued(t.Context()); err != nil || len(jobs) != 0 {
				t.Fatalf("cancelled rerun: %+v %v", jobs, err)
			}
			var events int
			if err := dbapi.QueryRowContext(t.Context(), database, `SELECT count(*) FROM job_events WHERE job_id='parent-job' AND event_type='CANCELLED'`).Scan(&events); err != nil {
				t.Fatal(err)
			}
			if events != 1 {
				t.Fatalf("cancel events=%d", events)
			}
		})
	}
}

func TestAttachmentRecoveryDoesNotTakeLiveLeaseAndStopsExhaustedExecution(t *testing.T) {
	for _, test := range []struct{ name, update, want, code string }{
		{"live", "leased_until_ms=21", "RUNNING", ""},
		{"deadline", "execution_deadline_at_ms=20", "FAILED", "ATTACHMENT_EXECUTION_TIMEOUT"},
		{"budget", "attempt_count=max_attempts", "FAILED", "ATTACHMENT_EXECUTION_EXHAUSTED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := attachmentExecutionFixture(t, "REVIEW_ARCADE_PARENT_VALIDATE", "RUNNING")
			metadataExec(t, database, `UPDATE jobs SET `+test.update+` WHERE id='parent-job'`)
			service := libraryservice.NewAttachmentExecutions(NewAttachmentExecutions(database), func() time.Time { return time.UnixMilli(20) })
			if err := service.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			var state, code string
			if err := dbapi.QueryRowContext(t.Context(), database, `SELECT state,COALESCE(error_code,'') FROM jobs WHERE id='parent-job'`).Scan(&state, &code); err != nil {
				t.Fatal(err)
			}
			if state != test.want || code != test.code {
				t.Fatalf("job=%s/%s", state, code)
			}
		})
	}
}

func TestAttachmentQueueFiltersBothKindsDueTimeAndBudget(t *testing.T) {
	database := metadataDatabase(t)
	for _, job := range []struct {
		id, kind, state    string
		available, attempt int
	}{
		{"parent-late", "REVIEW_ARCADE_PARENT_VALIDATE", "QUEUED", 20, 0},
		{"disc-early", "REVIEW_MULTI_DISC_VALIDATE", "QUEUED", 10, 0},
		{"future", "REVIEW_MULTI_DISC_VALIDATE", "QUEUED", 21, 0},
		{"exhausted", "REVIEW_MULTI_DISC_VALIDATE", "QUEUED", 1, 4},
		{"running", "REVIEW_ARCADE_PARENT_VALIDATE", "RUNNING", 1, 1},
		{"other", "IMPORT_GROUP", "QUEUED", 1, 0},
	} {
		metadataExec(t, database, `INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,
payload_json,cancellable,state,attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'IMPORT_ITEM','item',?,lower(hex(randomblob(32))),1,'{}',1,?,?,4,?,1,1)`, job.id, job.kind, job.state, job.attempt, job.available)
	}
	repository := NewAttachmentExecutions(database)
	jobs, err := repository.Queued(t.Context(), 20)
	if err != nil || len(jobs) != 2 || jobs[0].ID != "disc-early" || jobs[1].ID != "parent-late" {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if jobs, err := repository.Queued(ctx, 20); !errors.Is(err, context.Canceled) || jobs != nil {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
}

func TestOldAttachmentWorkerCannotWriteAfterLeaseExpiresOrTransfers(t *testing.T) {
	for _, transfer := range []bool{false, true} {
		t.Run(map[bool]string{false: "expired", true: "transferred"}[transfer], func(t *testing.T) {
			database := attachmentExecutionFixture(t, "REVIEW_ARCADE_PARENT_VALIDATE", "RUNNING")
			if transfer {
				metadataExec(t, database, `UPDATE jobs SET worker_id='new-worker',leased_until_ms=100 WHERE id='parent-job'`)
			}
			err := NewArcadeParentCommitRepository(database).FinishRetryable(t.Context(), libraryservice.ArcadeParentRetryableCommit{
				AttachmentID: "attachment", ItemID: "item", JobID: "parent-job", WorkerID: "worker", Code: "OLD_WORKER", DiagnosticsJSON: "{}", NowMS: 20,
			})
			if err == nil {
				t.Fatal("stale worker committed")
			}
			if err := HeartbeatAttachment(t.Context(), database, "parent-job", "worker", 20); err == nil {
				t.Fatal("stale worker renewed lease")
			}
			var state string
			var version, events int
			if err := dbapi.QueryRowContext(t.Context(), database, `SELECT state,version,(SELECT count(*) FROM job_events) FROM review_arcade_parent_attachments WHERE id='attachment'`).Scan(&state, &version, &events); err != nil {
				t.Fatal(err)
			}
			if state != "PENDING" || version != 1 || events != 0 {
				t.Fatalf("stale mutation=%s version=%d events=%d", state, version, events)
			}
		})
	}
}

func TestAttachmentRecoveryRollsBackTransitionAndEventsOnLateFailure(t *testing.T) {
	database := attachmentExecutionFixture(t, "REVIEW_ARCADE_PARENT_VALIDATE", "CANCEL_REQUESTED")
	fault := errors.New("recovery commit fault")
	err := NewAttachmentExecutions(database).WithRecovery(t.Context(), func(records libraryservice.AttachmentRecoveryRecords) error {
		jobs, err := records.Interrupted(t.Context(), 20)
		if err != nil {
			return err
		}
		if err := records.Transition(t.Context(), jobs[0], libraryservice.AttachmentTransition{State: "CANCELLED", Event: "CANCELLED", AvailableMS: 20, NowMS: 20}); err != nil {
			return err
		}
		return fault
	})
	if !errors.Is(err, fault) {
		t.Fatal(err)
	}
	var state, attachment string
	var events int
	if err := dbapi.QueryRowContext(t.Context(), database, `SELECT job.state,attachment.state,(SELECT count(*) FROM job_events)
FROM jobs job JOIN review_arcade_parent_attachments attachment ON attachment.job_id=job.id WHERE job.id='parent-job'`).Scan(&state, &attachment, &events); err != nil {
		t.Fatal(err)
	}
	if state != "CANCEL_REQUESTED" || attachment != "PENDING" || events != 0 {
		t.Fatalf("rollback=%s/%s events=%d", state, attachment, events)
	}
}

func assertRecoveredAttachmentExecution(t *testing.T, job libraryservice.AttachmentExecution) {
	t.Helper()
	if job.ID != "parent-job" || job.WorkerID != "" || job.ExecutionNo != 1 || job.Attempt != 1 || job.DeadlineMS == nil || *job.DeadlineMS != 2000 {
		t.Fatalf("recovery changed execution: %+v", job)
	}
}
