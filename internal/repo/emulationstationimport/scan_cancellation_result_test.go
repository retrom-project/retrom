package emulationstationimport

import (
	"database/sql"
	"reflect"
	"testing"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
)

func stagedCancellation(t *testing.T, running bool) (*sql.DB, emulationstationimportmodel.Summary, emulationstationimportmodel.LeaseSnapshot) {
	t.Helper()
	db, unit, value := scanDatabase(t)
	service := scanService(db)
	if err := service.Headers(t.Context(), unit, value); err != nil {
		t.Fatal(err)
	}
	if err := service.Items(t.Context(), unit, value.Items); err != nil {
		t.Fatal(err)
	}
	if !running {
		if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET state='QUEUED',leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL WHERE id=?`, unit.JobID); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := NewQueries(db).Get(t.Context(), unit.ImportID)
	if err != nil {
		t.Fatal(err)
	}
	return db, summary, readLease(t, db, unit.JobID)
}

func TestScanCancellationReturnsJobFactsFromSameCommit(t *testing.T) {
	t.Parallel()
	for _, running := range []bool{false, true} {
		t.Run(map[bool]string{false: "queued", true: "running"}[running], func(t *testing.T) {
			t.Parallel()
			db, summary, before := stagedCancellation(t, running)
			inputs := planTable(t, db, "job_input_snapshots")
			result, pending, err := scanCancellationService(db).CancelJob(t.Context(), application.JobCancellationRequest{JobID: before.JobID, ScopeID: summary.ID, Kind: before.Kind, ExpectedVersion: before.JobVersion, ActorID: "actor", Reason: " Stop "})
			if err != nil || pending != running {
				t.Fatalf("result=%#v pending=%v err=%v", result, pending, err)
			}
			after := readLease(t, db, before.JobID)
			if result.JobID != before.JobID || result.State != after.JobState || result.Version != after.JobVersion || result.ExecutionNo != before.ExecutionNo {
				t.Fatalf("response=%#v durable=%#v", result, after)
			}
			if after.JobVersion != before.JobVersion+1 || after.ImportVersion != before.ImportVersion+1 || after.ReleaseYearMax != before.ReleaseYearMax {
				t.Fatalf("versions/year=%#v", after)
			}
			if inputs != planTable(t, db, "job_input_snapshots") {
				t.Fatal("cancellation changed frozen input")
			}
			assertScanCancellationProjection(t, db, summary.ID, before, running)
			rows := planRows(t, db)
			if _, _, err := scanCancellationService(db).CancelJob(t.Context(), application.JobCancellationRequest{JobID: before.JobID, ScopeID: summary.ID, Kind: before.Kind, ExpectedVersion: after.JobVersion, ActorID: "actor", Reason: "Stop again"}); err == nil {
				t.Fatal("cancel replay accepted")
			}
			if !reflect.DeepEqual(rows, planRows(t, db)) {
				t.Fatal("cancel replay changed durable facts")
			}
		})
	}
}

func assertScanCancellationProjection(t *testing.T, db *sql.DB, id string, before emulationstationimportmodel.LeaseSnapshot, running bool) {
	t.Helper()
	var state, reason, actor string
	var items, events, payloads, counts int64
	err := db.QueryRowContext(t.Context(), `SELECT state,cancel_reason,
(SELECT actor_user_id FROM audit_events WHERE resource_id=plan.id AND action='EMULATIONSTATION_IMPORT_CANCEL_REQUESTED'),
(SELECT count(*) FROM emulationstation_import_items WHERE import_id=plan.id),
(SELECT count(*) FROM job_events WHERE job_id=plan.scan_job_id AND event_type='CANCEL_REQUESTED'),
(SELECT count(*) FROM jobs WHERE kind='PAYLOAD_RELEASE'),
gamelist_count+invalid_gamelist_count+collection_count+folder_entry_count+game_count+estimated_source_bytes+
mapped_collection_count+skipped_collection_count+processable_item_count+blocked_item_count+
media_warning_count+discovered_cover_count+discovered_video_count FROM emulationstation_imports plan WHERE id=?`, id).Scan(&state, &reason, &actor, &items, &events, &payloads, &counts)
	if err != nil {
		t.Fatal(err)
	}
	want, wantItems := "CANCELLED", int64(0)
	if running {
		want, wantItems = "CANCEL_REQUESTED", 1
	}
	if state != want || reason != "Stop" || actor != "actor" || items != wantItems || events != 1 || payloads != 0 || counts != 0 {
		t.Fatalf("projection=%s reason=%s actor=%s items=%d events=%d payloads=%d counts=%d", state, reason, actor, items, events, payloads, counts)
	}
	assertScanCancellationLease(t, db, before, running)
}

func assertScanCancellationLease(t *testing.T, db *sql.DB, before emulationstationimportmodel.LeaseSnapshot, running bool) {
	t.Helper()
	after := readLease(t, db, before.JobID)
	if running && (after.WorkerID != before.WorkerID || after.LeaseUntilMS != before.LeaseUntilMS) {
		t.Fatal("pending cancellation lost live owner")
	}
	if !running && (after.WorkerID != "" || after.LeaseUntilMS != 0) {
		t.Fatal("queued cancellation retained owner")
	}
}
