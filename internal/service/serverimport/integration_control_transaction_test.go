package serverimport_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	importpersistence "retrom/internal/repo/serverimport"
	importservice "retrom/internal/service/serverimport"
)

const controlActorID = "01980000-0000-7000-8000-00000000b001"

func failedControlImport(t *testing.T) (*Service, *sql.DB, Summary) {
	t.Helper()
	service, database, _ := archiveImportFixture(t)
	created, err := service.Create(t.Context(), CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, controlActorID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE server_bios_import_items SET state='READ_FAILED',completed_at_ms=?,updated_at_ms=? WHERE server_import_id=?`, created.UpdatedAtMS, created.UpdatedAtMS, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE server_imports SET state='FAILED',failed_item_count=catalog_item_count,last_error_code='INTERNAL_ERROR',completed_at_ms=? WHERE id=?`, created.UpdatedAtMS, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE jobs SET state='FAILED',finished_at_ms=?,error_code='INTERNAL_ERROR',error_retryable=1 WHERE id=?`, created.UpdatedAtMS, created.JobID); err != nil {
		t.Fatal(err)
	}
	return service, database, created
}

func TestRetryWriteRejectsSnapshotChangedAfterPreparation(t *testing.T) {
	_, database, created := failedControlImport(t)
	repository := importpersistence.NewControl(database)
	var before importservice.ControlSnapshot
	if err := repository.CommitWrite(t.Context(), func(scope importservice.ControlScope) error {
		var err error
		before, err = scope.Read.Current(t.Context(), created.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE server_imports SET version=version+1 WHERE id=?`, created.ID); err != nil {
		t.Fatal(err)
	}
	input := []byte(`{"schemaVersion":1}`)
	digest := sha256.Sum256(input)
	plan := importservice.ManualRetry{Before: before, Execution: before.Execution + 1, Input: input, InputDigest: fmt.Sprintf("%x", digest), Payload: []byte(`{"inputExecutionNo":2}`), Evidence: importservice.ControlEvidence{ActorID: controlActorID, AuditID: "retry-audit", Event: []byte(`{"schemaVersion":1,"executionNo":2}`), Now: created.UpdatedAtMS}}
	err := repository.CommitWrite(t.Context(), func(scope importservice.ControlScope) error { return scope.Write.Retry(t.Context(), plan) })
	if !errors.Is(err, ErrNotRetryable) {
		t.Fatalf("stale retry write: %v", err)
	}
	assertControlUnchanged(t, database, created.ID, "FAILED", created.Version+1)
	var itemState string
	if err := database.QueryRowContext(t.Context(), `SELECT state FROM server_bios_import_items WHERE server_import_id=?`, created.ID).Scan(&itemState); err != nil {
		t.Fatal(err)
	}
	if itemState != "READ_FAILED" {
		t.Fatalf("failed retry reset item to %s", itemState)
	}
}

type failingControlRepository struct {
	repository importservice.ControlRepository
}

func (repository failingControlRepository) CommitWrite(ctx context.Context, work func(importservice.ControlScope) error) error {
	return repository.repository.CommitWrite(ctx, func(scope importservice.ControlScope) error {
		if err := work(scope); err != nil {
			return err
		}
		return context.Canceled
	})
}

func TestImportControlLateFailureRollsBackEveryWrite(t *testing.T) {
	t.Run("retry", func(t *testing.T) {
		service, database, created := failedControlImport(t)
		control := importservice.NewControl(failingControlRepository{importpersistence.NewControl(database)}, map[string]string{"bios-root": service.RootDigestForTest("bios-root")}, time.Now)
		result, err := control.Retry(t.Context(), created.ID, created.Version, controlActorID)
		if !errors.Is(err, context.Canceled) || result.ID != "" {
			t.Fatalf("late retry: %+v %v", result, err)
		}
		assertControlUnchanged(t, database, created.ID, "FAILED", created.Version)
	})
	t.Run("cancel", func(t *testing.T) {
		service, database, _ := archiveImportFixture(t)
		created, err := service.Create(t.Context(), CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, controlActorID)
		if err != nil {
			t.Fatal(err)
		}
		control := importservice.NewControl(failingControlRepository{importpersistence.NewControl(database)}, nil, time.Now)
		result, pending, err := control.Cancel(t.Context(), created.ID, created.Version, "stop", controlActorID)
		if !errors.Is(err, context.Canceled) || result.ID != "" || pending {
			t.Fatalf("late cancel: %+v %v", result, err)
		}
		assertControlUnchanged(t, database, created.ID, "QUEUED", created.Version)
	})
}

func assertControlUnchanged(t *testing.T, database *sql.DB, id, state string, version int64) {
	t.Helper()
	var importState, jobState string
	var actualVersion, execution, snapshots, audits, events int64
	err := database.QueryRowContext(t.Context(), `SELECT import.state,import.version,job.state,job.execution_no,(SELECT count(*) FROM job_input_snapshots WHERE job_id=job.id),(SELECT count(*) FROM audit_events WHERE action IN ('SERVER_IMPORT_RETRIED','SERVER_IMPORT_CANCEL_REQUESTED')),(SELECT count(*) FROM job_events WHERE event_type IN ('MANUAL_RETRY','CANCEL_REQUESTED')) FROM server_imports import JOIN jobs job ON job.id=import.job_id WHERE import.id=?`, id).Scan(&importState, &actualVersion, &jobState, &execution, &snapshots, &audits, &events)
	if err != nil {
		t.Fatal(err)
	}
	if importState != state || jobState != state || actualVersion != version || execution != 1 || snapshots != 1 || audits != 0 || events != 0 {
		t.Fatalf("partial control transaction: import=%s/%d job=%s/%d snapshots=%d audits=%d events=%d", importState, actualVersion, jobState, execution, snapshots, audits, events)
	}
}
