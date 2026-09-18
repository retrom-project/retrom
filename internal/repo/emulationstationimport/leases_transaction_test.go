package emulationstationimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
)

func leaseDatabase(t *testing.T, importing bool) (*sql.DB, string) {
	t.Helper()
	if importing {
		db, summary := workflowDatabase(t, false)
		return db, *summary.ImportJobID
	}
	db := creationDatabase(t)
	if _, err := NewCreation(db).CommitCreation(t.Context(), creationPlan(0)); err != nil {
		t.Fatal(err)
	}
	return db, "job-0"
}

func TestLeaseClaimAndRenewKeepFrozenInputsAndBudget(t *testing.T) {
	t.Parallel()
	for _, importing := range []bool{false, true} {
		t.Run(map[bool]string{false: "scan", true: "import"}[importing], func(t *testing.T) {
			t.Parallel()
			assertClaimAndRenew(t, importing)
		})
	}
}

func readLease(t *testing.T, db *sql.DB, id string) emulationstationimportmodel.LeaseSnapshot {
	t.Helper()
	var result emulationstationimportmodel.LeaseSnapshot
	if err := NewLeases(db).WithLease(t.Context(), func(scope emulationstationimportmodel.LeaseScope) error {
		value, found, err := scope.Read.Current(t.Context(), id)
		if err == nil && !found {
			t.Fatal("linked execution missing")
		}
		result = value
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestLeaseClaimUsesOriginalDeadlineAfterAutomaticRetry(t *testing.T) {
	t.Parallel()
	db, id := leaseDatabase(t, false)
	if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET attempt_count=2,execution_started_at_ms=20,execution_deadline_at_ms=1050 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	service := emulationstationimportservice.NewLeases(NewLeases(db), func() time.Time { return time.UnixMilli(1000) })
	unit, found, err := service.Claim(t.Context())
	if err != nil || !found || unit.Attempt != 3 || unit.DeadlineAtMS != 1050 {
		t.Fatalf("claim=%#v found=%v error=%v", unit, found, err)
	}
	stored := readLease(t, db, id)
	if stored.StartedAtMS == nil || *stored.StartedAtMS != 20 || stored.LeaseUntilMS != 1050 {
		t.Fatalf("stored=%#v", stored)
	}
}

func TestLeaseRenewDoesNotReviveReplacedOrExpiredAttempt(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"worker", "attempt", "execution", "deadline", "lease", "aggregate", "link"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			db, id := leaseDatabase(t, false)
			service := emulationstationimportservice.NewLeases(NewLeases(db), func() time.Time { return time.UnixMilli(1000) })
			unit, found, err := service.Claim(t.Context())
			if err != nil || !found {
				t.Fatalf("claim=%v %v", found, err)
			}
			mutations := map[string]string{
				"worker":    `UPDATE jobs SET worker_id='replacement' WHERE id=?`,
				"attempt":   `UPDATE jobs SET attempt_count=2 WHERE id=?`,
				"execution": `UPDATE jobs SET execution_no=2 WHERE id=?`,
				"deadline":  `UPDATE jobs SET execution_deadline_at_ms=1000 WHERE id=?`,
				"lease":     `UPDATE jobs SET leased_until_ms=1000 WHERE id=?`,
				"aggregate": `UPDATE emulationstation_imports SET state='FAILED',phase=NULL,last_error_code='INTERNAL_ERROR',completed_at_ms=1000 WHERE scan_job_id=?`,
				"link":      `UPDATE emulationstation_imports SET scan_job_id='other' WHERE scan_job_id=?`,
			}
			if scenario == "link" {
				seedLeaseOrphan(t, db)
			}
			if _, err := db.ExecContext(t.Context(), mutations[scenario], id); err != nil {
				t.Fatal(err)
			}
			before := planRows(t, db)
			state, err := service.Renew(t.Context(), unit)
			if err != nil || state != emulationstationimportmodel.LeaseLost {
				t.Fatalf("renew=%s error=%v", state, err)
			}
			if !reflect.DeepEqual(planRows(t, db), before) {
				t.Fatal("rejected renewal wrote state")
			}
		})
	}
}

func TestLeaseClaimConcurrentWorkersOnlyOneOwnsAttempt(t *testing.T) {
	t.Parallel()
	db, _ := leaseDatabase(t, false)
	service := emulationstationimportservice.NewLeases(NewLeases(db), func() time.Time { return time.UnixMilli(1000) })
	type outcome struct {
		unit  emulationstationimportmodel.Execution
		found bool
		err   error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			unit, found, err := service.Claim(context.Background())
			results <- outcome{unit, found, err}
		}()
	}
	close(start)
	claimed := 0
	for range 2 {
		result := <-results
		if result.found {
			claimed++
			if result.err != nil || result.unit.WorkerID == "" {
				t.Fatalf("claim=%#v", result)
			}
		}
	}
	var events int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM job_events WHERE event_type='STARTED'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if claimed != 1 || events != 1 {
		t.Fatalf("claims=%d events=%d", claimed, events)
	}
}

func assertClaimAndRenew(t *testing.T, importing bool) {
	t.Helper()
	db, id := leaseDatabase(t, importing)
	frozenInput := planTable(t, db, "job_input_snapshots")
	now := int64(1000)
	service := emulationstationimportservice.NewLeases(NewLeases(db), func() time.Time { return time.UnixMilli(now) })
	unit, found, err := service.Claim(t.Context())
	if err != nil || !found || unit.JobID != id || unit.Attempt != 1 || unit.WorkerID == "" || unit.ReleaseYearMax != 1971 {
		t.Fatalf("claim=%#v found=%v error=%v", unit, found, err)
	}
	before := readLease(t, db, id)
	assertClaimSnapshot(t, db, id, unit, before)
	now = 2000
	state, err := service.Renew(t.Context(), unit)
	if err != nil || state != emulationstationimportmodel.LeaseActive {
		t.Fatalf("renew=%s error=%v", state, err)
	}
	after := readLease(t, db, id)
	if after.Execution != unit || after.JobVersion != before.JobVersion+1 || after.ImportVersion != before.ImportVersion || after.LeaseUntilMS != 62000 {
		t.Fatalf("renewed=%#v", after)
	}
	if planTable(t, db, "job_input_snapshots") != frozenInput {
		t.Fatal("lease changed immutable input")
	}
}

func seedLeaseOrphan(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), `INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES('other','EMULATIONSTATION_IMPORT','import-0','SERVER_EMULATIONSTATION_SCAN','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',1,'{"inputExecutionNo":1}',1,'QUEUED',0,4,1,0,0,0)`); err != nil {
		t.Fatal(err)
	}
}

func assertClaimSnapshot(t *testing.T, db *sql.DB, id string, unit emulationstationimportmodel.Execution, before emulationstationimportmodel.LeaseSnapshot) {
	t.Helper()
	if before.Execution != unit || before.JobVersion != 2 || before.LeaseUntilMS != 61000 || before.StartedAtMS == nil || *before.StartedAtMS != 1000 {
		t.Fatalf("persisted=%#v", before)
	}
	var data string
	if err := db.QueryRowContext(t.Context(), `SELECT data_json FROM job_events WHERE job_id=? AND event_type='STARTED'`, id).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(event, map[string]any{"schemaVersion": float64(1), "executionNo": float64(1), "attempt": float64(1)}) {
		t.Fatalf("event=%s", data)
	}
}
