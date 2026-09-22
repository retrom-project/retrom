package sourceimport

import (
	"testing"
	"time"

	application "retrom/internal/service/sourceimport"
)

func TestCancellingScanDoesNotReserveImportExecutionCapacity(t *testing.T) {
	t.Parallel()
	db := workflowDatabase(t)
	if err := NewCreation(db).WithCreate(t.Context(), func(writer application.CreationWriter) error {
		_, err := writer.Insert(t.Context(), creationPlan(1))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(
		t.Context(),
		`UPDATE jobs SET state='RUNNING',attempt_count=1,worker_id='scanner',
leased_until_ms=90,heartbeat_at_ms=2,execution_started_at_ms=2,execution_deadline_at_ms=100 WHERE id='job-1'`,
	); err != nil {
		t.Fatal(err)
	}
	service := application.NewWorkflowControl(NewWorkflowControl(db), func() time.Time { return time.UnixMilli(10) })
	if _, pending, err := service.CancelJob(t.Context(), application.JobCancellationRequest{
		JobID: "job-1", ScopeID: "import-1", Kind: "IMPORT_SCAN", ExpectedVersion: 1, Reason: "Stop", ActorID: "actor",
	}); err != nil || !pending {
		t.Fatalf("request scan cancellation: %v %v", pending, err)
	}
	var before application.WorkflowSnapshot
	if err := NewWorkflowControl(db).WithControl(t.Context(), func(scope application.WorkflowScope) error {
		var err error
		before, err = scope.Read.Current(t.Context(), "import-0")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if before.OtherActive {
		t.Fatal("canceling scan occupies import capacity")
	}
	result, err := service.Retry(t.Context(), "import-0", before.Summary.Version, "actor")
	if err != nil || result.State != "QUEUED" {
		t.Fatalf("retry blocked by independent scan: %#v %v", result, err)
	}
}
