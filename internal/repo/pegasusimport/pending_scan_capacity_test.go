package pegasusimport

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	pegasusimportmodel "retrom/internal/model/pegasusimport"
	pegasusimportservice "retrom/internal/service/pegasusimport"
)

func TestCreationCountsPendingScanCancellationUntilItCloses(t *testing.T) {
	t.Parallel()
	database := pendingScanCapacityDatabase(t)
	repository := NewCreation(database)
	if err := repository.WithCreate(t.Context(), func(writer pegasusimportmodel.CreationWriter) error {
		count, err := writer.PendingPlans(t.Context())
		if err != nil {
			return err
		}
		if count != 20 {
			t.Errorf("canceling scan released plan capacity: got %d, want 20", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	control := pegasusimportservice.NewWorkflowControl(NewWorkflowControl(database), func() time.Time { return time.UnixMilli(10) })
	if _, pending, err := control.CancelJob(t.Context(), pegasusimportservice.JobCancellationRequest{
		JobID: "job-1", ScopeID: "import-1", Kind: "SERVER_PEGASUS_SCAN", ExpectedVersion: 1,
		Reason: "Stop queued scan", ActorID: "actor",
	}); err != nil || pending {
		t.Fatalf("close queued scan: pending=%v err=%v", pending, err)
	}
	if err := repository.WithCreate(t.Context(), func(writer pegasusimportmodel.CreationWriter) error {
		count, err := writer.PendingPlans(t.Context())
		if err != nil {
			return err
		}
		if count != 19 {
			t.Errorf("closed scan still occupies capacity or pending scan was omitted: %d", count)
		}
		_, err = writer.Insert(t.Context(), creationPlan(20))
		return err
	}); err != nil {
		t.Fatalf("reuse terminal scan capacity: %v", err)
	}
	assertScanCapacityCounts(t, database, 21, 2)
}

func TestCreationFinalInsertCannotBypassPendingScanCapacity(t *testing.T) {
	t.Parallel()
	database := pendingScanCapacityDatabase(t)
	err := NewCreation(database).WithCreate(t.Context(), func(writer pegasusimportmodel.CreationWriter) error {
		_, err := writer.Insert(t.Context(), creationPlan(20))
		return err
	})
	if !errors.Is(err, pegasusimportmodel.ErrActive) {
		t.Fatalf("21st unstarted plan accepted while scan cancellation is pending: %v", err)
	}
	assertScanCapacityCounts(t, database, 20, 1)
}

func pendingScanCapacityDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database := creationDatabase(t)
	repository := NewCreation(database)
	for index := range 20 {
		if err := repository.WithCreate(t.Context(), func(writer pegasusimportmodel.CreationWriter) error {
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
	control := pegasusimportservice.NewWorkflowControl(NewWorkflowControl(database), func() time.Time { return time.UnixMilli(10) })
	if _, pending, err := control.CancelJob(t.Context(), pegasusimportservice.JobCancellationRequest{
		JobID: "job-0", ScopeID: "import-0", Kind: "SERVER_PEGASUS_SCAN", ExpectedVersion: 1,
		Reason: "Stop running scan", ActorID: "actor",
	}); err != nil || !pending {
		t.Fatalf("request running scan cancellation: pending=%v err=%v", pending, err)
	}
	return database
}

func assertScanCapacityCounts(t *testing.T, database *sql.DB, plans, cancellations int) {
	t.Helper()
	queries := map[string]int{
		`SELECT count(*) FROM pegasus_imports`:                                    plans,
		`SELECT count(*) FROM jobs WHERE scope_type='PEGASUS_IMPORT'`:             plans,
		`SELECT count(*) FROM job_input_snapshots`:                                plans,
		`SELECT count(*) FROM job_events WHERE scope_type='PEGASUS_IMPORT'`:       plans + cancellations,
		`SELECT count(*) FROM audit_events WHERE action='PEGASUS_IMPORT_CREATED'`: plans,
	}
	for query, expected := range queries {
		var count int
		if err := database.QueryRowContext(t.Context(), query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != expected {
			t.Fatalf("%s: got %d, want %d", query, count, expected)
		}
	}
}
