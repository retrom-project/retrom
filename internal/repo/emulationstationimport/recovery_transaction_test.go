package emulationstationimport

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
)

func recoveryDatabase(t *testing.T, importing, staging bool) (*sql.DB, emulationstationimportmodel.Execution) {
	t.Helper()
	db, _ := leaseDatabase(t, importing)
	unit, found, err := application.NewLeases(NewLeases(db), func() time.Time { return time.UnixMilli(1000) }).Claim(t.Context())
	if err != nil || !found {
		t.Fatalf("claim=%v error=%v", found, err)
	}
	if staging {
		seedPlanProjection(t, db)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET leased_until_ms=1500 WHERE id=?`, unit.JobID); err != nil {
		t.Fatal(err)
	}
	return db, unit
}

func TestRecoveryPreservesFrozenImportProgressAcrossRetry(t *testing.T) {
	t.Parallel()
	db, unit := recoveryDatabase(t, true, false)
	if _, err := db.ExecContext(t.Context(), `UPDATE emulationstation_import_items SET execution_state='COPYING' WHERE id='start-item-2'`); err != nil {
		t.Fatal(err)
	}
	inputs := planTable(t, db, "job_input_snapshots")
	items := planTable(t, db, "emulationstation_import_items")
	files := planTable(t, db, "emulationstation_import_item_files")
	tags := planTable(t, db, "tags")
	if err := application.NewRecovery(NewRecovery(db), func() time.Time { return time.UnixMilli(1500) }).Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	after := readLease(t, db, unit.JobID)
	assertQueuedRecovery(t, after, unit)
	if inputs != planTable(t, db, "job_input_snapshots") || items != planTable(t, db, "emulationstation_import_items") || files != planTable(t, db, "emulationstation_import_item_files") || tags != planTable(t, db, "tags") {
		t.Fatal("recovery rewrote immutable input, item progress, copied files or tags")
	}
	assertRecoveryEvent(t, db, unit.JobID, "RETRY_SCHEDULED", map[string]any{"schemaVersion": float64(1), "executionNo": float64(1), "attempt": float64(1), "retryAtMs": float64(2500), "errorCode": "EMULATIONSTATION_WORKER_LEASE_EXPIRED", "errorRetryable": true})
	before := planRows(t, db)
	if err := application.NewRecovery(NewRecovery(db), func() time.Time { return time.UnixMilli(1500) }).Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, planRows(t, db)) {
		t.Fatal("recovery replay rewrote queued execution")
	}
}

func TestRecoveryClearsUnpublishedScanOnRetryAndFailure(t *testing.T) {
	t.Parallel()
	for _, terminal := range []bool{false, true} {
		t.Run(map[bool]string{false: "retry", true: "failed"}[terminal], func(t *testing.T) {
			t.Parallel()
			db, unit := recoveryDatabase(t, false, true)
			if terminal {
				if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET execution_deadline_at_ms=2500 WHERE id=?`, unit.JobID); err != nil {
					t.Fatal(err)
				}
			}
			inputs := planTable(t, db, "job_input_snapshots")
			if err := application.NewRecovery(NewRecovery(db), func() time.Time { return time.UnixMilli(1500) }).Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			assertClearedScan(t, db, unit, terminal)
			if inputs != planTable(t, db, "job_input_snapshots") {
				t.Fatal("scan recovery rewrote input")
			}
		})
	}
}

func assertClearedScan(t *testing.T, db *sql.DB, unit emulationstationimportmodel.Execution, terminal bool) {
	t.Helper()
	var state string
	var count, payloads int64
	if err := db.QueryRowContext(t.Context(), `SELECT state,gamelist_count+invalid_gamelist_count+collection_count+folder_entry_count+game_count+estimated_source_bytes+mapped_collection_count+skipped_collection_count+processable_item_count+blocked_item_count+media_warning_count+discovered_cover_count+discovered_video_count,(SELECT count(*) FROM jobs WHERE kind='PAYLOAD_RELEASE') FROM emulationstation_imports WHERE id=?`, unit.ImportID).Scan(&state, &count, &payloads); err != nil {
		t.Fatal(err)
	}
	want := "SCANNING"
	if terminal {
		want = "FAILED"
	}
	if state != want || count != 0 || payloads != 0 {
		t.Fatalf("scan=%s counters=%d payloads=%d", state, count, payloads)
	}
	for _, table := range []string{"emulationstation_import_gamelists", "emulationstation_import_collections", "emulationstation_import_items", "emulationstation_import_item_files", "emulationstation_import_item_assets"} {
		if planTable(t, db, table) != "[]" {
			t.Fatalf("retained partial %s", table)
		}
	}
}

func assertRecoveryEvent(t *testing.T, db *sql.DB, id, kind string, want map[string]any) {
	t.Helper()
	var encoded string
	if err := db.QueryRowContext(t.Context(), `SELECT data_json FROM job_events WHERE job_id=? AND event_type=?`, id, kind).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(encoded), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event=%s", encoded)
	}
}

func TestRecoveryTerminatesQueuedBudgetAndPreservesFinishedImportItems(t *testing.T) {
	t.Parallel()
	db, unit := recoveryDatabase(t, true, false)
	if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET state='QUEUED',worker_id=NULL,leased_until_ms=NULL,heartbeat_at_ms=NULL,attempt_count=max_attempts WHERE id=?`, unit.JobID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE emulationstation_imports SET state='QUEUED',phase=NULL WHERE id=?`, unit.ImportID); err != nil {
		t.Fatal(err)
	}
	if err := application.NewRecovery(NewRecovery(db), func() time.Time { return time.UnixMilli(1500) }).Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	summary, err := NewQueries(db).Get(t.Context(), unit.ImportID)
	if err != nil || summary.State != "FAILED" || summary.Counts.Failed != 1 || summary.Counts.SkippedMapping != 1 || summary.Counts.Blocked != 1 {
		t.Fatalf("summary=%#v error=%v", summary, err)
	}
	assertRecoveryEvent(t, db, unit.JobID, "FAILED", map[string]any{"schemaVersion": float64(1), "code": "EMULATIONSTATION_WORKER_ATTEMPTS_EXHAUSTED"})
	var releases int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM jobs WHERE kind='PAYLOAD_RELEASE'`).Scan(&releases); err != nil {
		t.Fatal(err)
	}
	if releases != 3 {
		t.Fatalf("terminal release jobs=%d", releases)
	}
}

func assertQueuedRecovery(t *testing.T, after emulationstationimportmodel.LeaseSnapshot, unit emulationstationimportmodel.Execution) {
	t.Helper()
	if after.JobState != "QUEUED" || after.ImportState != "QUEUED" || after.AvailableAtMS != 2500 || after.DeadlineAtMS != unit.DeadlineAtMS || after.Attempt != unit.Attempt || after.ReleaseYearMax != unit.ReleaseYearMax || after.WorkerID != "" {
		t.Fatalf("recovered=%#v", after)
	}
}
