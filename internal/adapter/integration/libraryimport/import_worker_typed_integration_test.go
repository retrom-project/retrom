//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/testkit/testsupport"
)

func typedImportExecutions(service *Service) *application.ImportExecutions {
	return application.NewImportExecutions(repository.NewImportExecutions(service.database), service.now)
}

func TestTypedImportWorkerClaimsFrozenInput(t *testing.T) {
	service, plan := preparedCommitFixture(t)
	admission := application.NewImportAdmissions(
		repository.NewImportAdmissions(service.database),
		nil,
		service.tags,
		application.ImportAdmissionOptions{Now: service.now},
	)
	created, err := admission.Queue(t.Context(), plan.Request)
	if err != nil {
		t.Fatal(err)
	}
	work, found, err := typedImportExecutions(service).Claim(t.Context(), created.JobID)
	if err != nil || !found || work.Execution.JobID != created.JobID || work.Execution.ImportID != created.ImportJobID ||
		work.Execution.WorkerID == "" ||
		work.Execution.Attempt != 1 ||
		work.Request.UploadID != plan.Upload.ID ||
		work.Execution.DeadlineMS-work.Execution.StartedAtMS != application.ImportExecutionBudget.Milliseconds() {
		t.Fatalf("claim work=%+v found=%t error=%v", work, found, err)
	}
}

func TestTypedImportWorkerRejectsExpiredProgressAndRenew(t *testing.T) {
	for _, change := range []struct{ name, sql string }{
		{"execution", `UPDATE jobs SET execution_no=execution_no+1 WHERE id=?`},
		{"attempt", `UPDATE jobs SET attempt_count=attempt_count+1 WHERE id=?`},
		{"lease", `UPDATE jobs SET leased_until_ms=1 WHERE id=?`},
		{"deadline", `UPDATE jobs SET execution_deadline_at_ms=1 WHERE id=?`},
	} {
		t.Run(
			change.name,
			func(t *testing.T) {
				service, work := workerAuthorityFixture(t)
				if _, err := service.database.ExecContext(t.Context(), change.sql, work.jobID); err != nil {
					t.Fatal(err)
				}
				var beforeLease, afterLease int64
				if err := service.database.QueryRowContext(t.Context(), `SELECT leased_until_ms FROM jobs WHERE id=?`, work.jobID).Scan(
					&beforeLease,
				); err != nil {
					t.Fatal(err)
				}
				current := typedImportExecutions(service)
				if err := current.Progress(t.Context(), *work.creationIntent(), 1); !errors.Is(err, ErrVersionConflict) {
					t.Fatalf("progress stale error=%v", err)
				}
				if cancelled, err := current.Renew(t.Context(), *work.creationIntent()); cancelled || !errors.Is(err, ErrVersionConflict) {
					t.Fatalf("renew stale cancelled=%t error=%v", cancelled, err)
				}
				if err := service.database.QueryRowContext(t.Context(), `SELECT leased_until_ms FROM jobs WHERE id=?`, work.jobID).Scan(
					&afterLease,
				); err != nil {
					t.Fatal(err)
				}
				if afterLease != beforeLease {
					t.Fatalf("stale renew changed lease: before=%d after=%d", beforeLease, afterLease)
				}
				var count int
				if err := service.database.QueryRowContext(
					t.Context(),
					`SELECT count(*) FROM job_events WHERE job_id=? AND event_type='PROGRESS'`,
					work.jobID,
				).Scan(
					&count,
				); err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Fatalf("stale progress events=%d", count)
				}
			},
		)
	}
}

func TestTypedImportWorkerRecoveryPreservesBudgetAndLiveOwner(t *testing.T) {
	service, work := workerAuthorityFixture(t)
	executions := typedImportExecutions(service)
	if err := executions.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	var state, owner string
	if err := service.database.QueryRowContext(t.Context(), `SELECT state,worker_id FROM jobs WHERE id=?`, work.jobID).Scan(
		&state,
		&owner,
	); err != nil {
		t.Fatal(err)
	}
	if state != "RUNNING" || owner != work.workerID {
		t.Fatalf("live worker stolen: %s/%s", state, owner)
	}
	if _, err := service.database.ExecContext(t.Context(), `UPDATE jobs SET leased_until_ms=1 WHERE id=?`, work.jobID); err != nil {
		t.Fatal(err)
	}
	if err := executions.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	next, found, err := executions.Claim(t.Context(), work.jobID)
	if err != nil || !found || next.Execution.Attempt != int64(work.attempt+1) ||
		next.Execution.WorkerID == work.workerID ||
		next.Execution.StartedAtMS != work.executionStartedAt ||
		next.Execution.DeadlineMS != work.executionDeadline ||
		next.Execution.ExecutionNo != work.executionNo {
		t.Fatalf("recovered execution=%+v found=%t error=%v", next, found, err)
	}
}

func TestTypedImportWorkerFailureAndPayloadShareTransaction(t *testing.T) {
	service, work := workerAuthorityFixture(t)
	cause := errors.New("payload scheduling unavailable")
	writes, release := int64(0), 0
	source := service.database
	service.database = testsupport.OpenSQLFaultDatabase(
		t,
		source,
		testsupport.SQLFaultHooks{
			BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
				if strings.HasPrefix(strings.TrimSpace(query), "INSERT INTO jobs(") && strings.Contains(query, "'PAYLOAD_RELEASE'") {
					release++
					return cause
				}
				return nil
			},
			AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
				if strings.HasPrefix(strings.TrimSpace(query), "UPDATE jobs SET state=") && len(args) > 0 &&
					args[0].Value == "FAILED" {
					count, err := result.RowsAffected()
					if err != nil {
						return nil, err
					}
					writes += count
				}
				return result, nil
			},
		},
	)
	err := typedImportExecutions(service).Fail(t.Context(), *work.creationIntent(), ErrInvalid)
	if !errors.Is(err, cause) || writes != 1 || release != 1 {
		t.Fatalf("atomic failure: writes=%d release=%d err=%v", writes, release, err)
	}
	var state, parent, payload string
	if err := source.QueryRowContext(
		t.Context(),
		`SELECT job.state,parent.state,parent.payload_state FROM jobs job JOIN import_jobs parent ON
 parent.id=job.scope_id WHERE job.id=?`,
		work.jobID,
	).Scan(
		&state,
		&parent,
		&payload,
	); err != nil {
		t.Fatal(err)
	}
	if state != "RUNNING" || parent != "RUNNING" || payload != "RETAINED" {
		t.Fatalf("partial failure: %s/%s/%s", state, parent, payload)
	}
	service.database = source
	if err := typedImportExecutions(service).Fail(t.Context(), *work.creationIntent(), ErrInvalid); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := source.QueryRowContext(
		t.Context(),
		`SELECT count(*) FROM jobs WHERE kind='PAYLOAD_RELEASE' AND scope_id=?`,
		work.importID,
	).Scan(
		&count,
	); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("retry release jobs=%d", count)
	}
}
