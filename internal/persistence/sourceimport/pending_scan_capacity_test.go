package sourceimport

import (
	"testing"
	"time"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/sourceimport"
)

func TestCreationQueuesWhileScanCancellationIsPending(t *testing.T) {
	t.Parallel()
	db := pendingScanCapacityDatabase(t)
	if err := NewCreation(db).WithCreate(t.Context(), func(writer application.CreationWriter) error {
		_, err := writer.Insert(t.Context(), creationPlan(20))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	assertScanCapacityCounts(t, db, 21, 1)
}

func pendingScanCapacityDatabase(t *testing.T) dbapi.DB {
	t.Helper()
	database := creationDatabase(t)
	repository := NewCreation(database)
	for index := range 20 {
		if err := repository.WithCreate(t.Context(), func(writer application.CreationWriter) error {
			_, err := writer.Insert(t.Context(), creationPlan(index))
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.ExecContext(t.Context(), `
UPDATE jobs SET state='RUNNING',attempt_count=1,worker_id='scanner',leased_until_ms=90,
heartbeat_at_ms=2,execution_started_at_ms=2,execution_deadline_at_ms=100 WHERE id='job-0'`); err != nil {
		t.Fatal(err)
	}
	control := application.NewWorkflowControl(NewWorkflowControl(database), func() time.Time { return time.UnixMilli(10) })
	if _, pending, err := control.CancelJob(t.Context(), application.JobCancellationRequest{
		JobID: "job-0", ScopeID: "import-0", Kind: "IMPORT_SCAN",
		Reason: "Stop running scan", ActorID: "actor",
	}); err != nil || !pending {
		t.Fatalf("request running scan cancellation: pending=%v err=%v", pending, err)
	}
	return database
}

func assertScanCapacityCounts(t *testing.T, database dbapi.DB, plans, cancellations int) {
	t.Helper()
	queries := map[string]int{
		`SELECT count(*) FROM source_imports`:                                    plans,
		`SELECT count(*) FROM jobs WHERE scope_type='SOURCE_IMPORT'`:             plans,
		`SELECT count(*) FROM job_input_snapshots`:                               plans,
		`SELECT count(*) FROM job_events WHERE scope_type='SOURCE_IMPORT'`:       plans + cancellations,
		`SELECT count(*) FROM audit_events WHERE action='SOURCE_IMPORT_CREATED'`: plans,
	}
	for query, expected := range queries {
		var count int
		if err := dbapi.QueryRowContext(t.Context(), database, query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != expected {
			t.Fatalf("%s: got %d, want %d", query, count, expected)
		}
	}
}
