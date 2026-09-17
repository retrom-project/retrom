package emulationstationimport

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"reflect"
	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
	"testing"
	"time"
)

func workflowDatabase(t *testing.T, retry bool) (*sql.DB, emulationstationimportmodel.Summary) {
	t.Helper()
	db := startDatabase(t)
	summary, _, err := emulationstationimportservice.NewStarter(NewStarter(db), verifiedStartSource{database: db}, func() time.Time { return time.UnixMilli(10) }).Start(t.Context(), "import-0", 2, mappingActor)
	if err != nil {
		t.Fatal(err)
	}
	if retry {
		if _, err := db.ExecContext(t.Context(), `UPDATE emulationstation_import_items SET execution_state='COMMIT_FAILED',error_code='INTERNAL_ERROR',
error_details_json='{"schemaVersion":1,"stage":"COMMIT"}',retryable=1,completed_at_ms=11,version=version+1,updated_at_ms=11
WHERE import_id='import-0' AND execution_state='PENDING';
UPDATE emulationstation_imports SET state='PARTIAL_FAILURE',failed_item_count=1,retryable=1,completed_at_ms=11,
phase=NULL,version=version+1,updated_at_ms=11 WHERE id='import-0';
UPDATE jobs SET state='SUCCEEDED',finished_at_ms=11,attempt_count=2,execution_started_at_ms=10,
execution_deadline_at_ms=20,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,
version=version+1,updated_at_ms=11 WHERE id=?`, *summary.ImportJobID); err != nil {
			t.Fatal(err)
		}
		summary, err = NewQueries(db).Get(t.Context(), summary.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	return db, summary
}

func TestWorkflowRetryKeepsFrozenHistoryAndResetsOnlyRetryableRows(t *testing.T) {
	t.Parallel()
	db, before := workflowDatabase(t, true)
	originalSnapshot := planTable(t, db, "job_input_snapshots")
	tags := planTable(t, db, "tags")
	gamelists := planTable(t, db, "emulationstation_import_gamelists")
	service := emulationstationimportservice.NewWorkflowControl(NewWorkflowControl(db), verifiedStartSource{database: db}, func() time.Time { return time.UnixMilli(before.ExpiresAtMS + 1) })
	after, err := service.Retry(t.Context(), before.ID, before.Version, mappingActor)
	if err != nil || after.State != "QUEUED" || after.Counts.Failed != 0 || after.Version != before.Version+1 {
		t.Fatalf("retry=%#v error=%v", after, err)
	}
	if tags != planTable(t, db, "tags") || gamelists != planTable(t, db, "emulationstation_import_gamelists") {
		t.Fatal("retry changed frozen evidence or tags")
	}
	assertRetriedJob(t, db, *after.ImportJobID, originalSnapshot)
	assertRetriedRows(t, db)
}

func assertRetriedJob(t *testing.T, db *sql.DB, id, original string) {
	t.Helper()
	var execution, attempt, inputs, audits, year int
	var reset bool
	var encoded, digest string
	if err := db.QueryRowContext(t.Context(), `SELECT execution_no,attempt_count,
execution_started_at_ms IS NULL AND execution_deadline_at_ms IS NULL AND leased_until_ms IS NULL
AND heartbeat_at_ms IS NULL AND finished_at_ms IS NULL AND worker_id IS NULL AND error_code IS NULL
AND error_retryable IS NULL AND cancel_requested_at_ms IS NULL AND cancel_reason IS NULL,
(SELECT count(*) FROM job_input_snapshots WHERE job_id=jobs.id),
(SELECT count(*) FROM audit_events WHERE resource_id='import-0' AND action='EMULATIONSTATION_IMPORT_RETRIED' AND actor_user_id=?),
(SELECT release_year_max FROM emulationstation_imports WHERE id='import-0'),
(SELECT input_json FROM job_input_snapshots WHERE job_id=jobs.id AND execution_no=2),
(SELECT input_digest FROM job_input_snapshots WHERE job_id=jobs.id AND execution_no=2)
FROM jobs WHERE id=?`, mappingActor, id).Scan(&execution, &attempt, &reset, &inputs, &audits, &year, &encoded, &digest); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(encoded))
	if execution != 2 || attempt != 0 || !reset || inputs != 2 || audits != 1 || year != 1971 || digest != hex.EncodeToString(sum[:]) {
		t.Fatalf("job exec=%d attempt=%d reset=%v inputs=%d audits=%d year=%d", execution, attempt, reset, inputs, audits, year)
	}
	var before, after [][]any
	if err := json.Unmarshal([]byte(original), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(planTable(t, db, "job_input_snapshots")), &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after[:len(before)]) {
		t.Fatal("retry rewrote an earlier immutable input")
	}
}

func TestWorkflowQueuedCancellationIsAtomicAndPreservesTerminalRows(t *testing.T) {
	t.Parallel()
	db, before := workflowDatabase(t, false)
	service := emulationstationimportservice.NewWorkflowControl(NewWorkflowControl(db), verifiedStartSource{database: db}, func() time.Time { return time.UnixMilli(12) })
	after, pending, err := service.Cancel(t.Context(), before.ID, before.Version, " Stop ", mappingActor)
	if err != nil || pending || after.State != "CANCELLED" || after.Counts.Cancelled != 1 || after.Counts.Blocked != 1 || after.Counts.SkippedMapping != 1 {
		t.Fatalf("cancel=%#v pending=%v error=%v", after, pending, err)
	}
	var state, reason string
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT state,cancel_reason,(SELECT count(*) FROM jobs WHERE kind='PAYLOAD_RELEASE') FROM jobs WHERE id=?`, *after.ImportJobID).Scan(&state, &reason, &count); err != nil {
		t.Fatal(err)
	}
	if state != "CANCELLED" || reason != "Stop" || count != 3 {
		t.Fatalf("cancel job=%s reason=%s releases=%d", state, reason, count)
	}
}

func assertRetriedRows(t *testing.T, db *sql.DB) {
	t.Helper()
	var state, payload string
	var cleared bool
	if err := db.QueryRowContext(t.Context(), `SELECT execution_state,payload_state,error_code IS NULL AND error_details_json IS NULL AND completed_at_ms IS NULL AND retryable=0 FROM emulationstation_import_items WHERE id='start-item-2'`).Scan(&state, &payload, &cleared); err != nil {
		t.Fatal(err)
	}
	if state != "PENDING" || payload != "RETAINED" || !cleared {
		t.Fatalf("retried row=%s/%s cleared=%v", state, payload, cleared)
	}
	for _, id := range []string{"start-item-0", "start-item-1"} {
		if err := db.QueryRowContext(t.Context(), `SELECT execution_state,payload_state FROM emulationstation_import_items WHERE id=?`, id).Scan(&state, &payload); err != nil {
			t.Fatal(err)
		}
		if state != "BLOCKED_SOURCE" && state != "SKIPPED_MAPPING" || payload != "RELEASING" {
			t.Fatalf("protected row %s=%s/%s", id, state, payload)
		}
	}
}
