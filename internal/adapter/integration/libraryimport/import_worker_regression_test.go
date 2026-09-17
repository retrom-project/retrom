//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	libraryimportmodel "retrom/internal/model/libraryimport"
	repository "retrom/internal/repo/libraryimport"
	libraryimportservice "retrom/internal/service/libraryimport"
	"retrom/internal/testkit/testsupport"
)

func workerAuthorityFixture(t *testing.T) (*Service, queuedCreationWork) {
	t.Helper()
	service, plan := preparedCommitFixture(t)
	admissions := libraryimportservice.NewImportAdmissions(repository.NewImportAdmissions(service.database), nil, service.tags,
		libraryimportmodel.ImportAdmissionOptions{Now: service.now})
	created, err := admissions.Queue(t.Context(), plan.Request)
	if err != nil {
		t.Fatal(err)
	}
	work, err := service.claimImportGroup(t.Context(), created.JobID)
	if err != nil {
		t.Fatal(err)
	}
	return service, work
}

func TestImportWorkerProgressRejectsStaleExecution(t *testing.T) {
	for _, change := range []struct{ name, statement string }{
		{"execution", `UPDATE jobs SET execution_no=execution_no+1 WHERE id=?`},
		{"attempt", `UPDATE jobs SET attempt_count=attempt_count+1 WHERE id=?`},
		{"lease", `UPDATE jobs SET leased_until_ms=1 WHERE id=?`},
		{"deadline", `UPDATE jobs SET execution_deadline_at_ms=1 WHERE id=?`},
	} {
		t.Run(
			change.name,
			func(t *testing.T) {
				service, work := workerAuthorityFixture(t)
				if _, err := service.database.ExecContext(t.Context(), change.statement, work.jobID); err != nil {
					t.Fatal(err)
				}
				err := service.recordImportGroupProgress(t.Context(), work, "PERSISTING", 1)
				var count int
				if queryErr := service.database.QueryRowContext(
					t.Context(),
					`SELECT count(*) FROM job_events WHERE job_id=? AND event_type='PROGRESS'`,
					work.jobID,
				).Scan(
					&count,
				); queryErr != nil {
					t.Fatal(queryErr)
				}
				if err == nil || count != 0 {
					t.Fatalf("stale %s progress accepted: events=%d error=%v", change.name, count, err)
				}
			},
		)
	}
}

func TestImportWorkerRecoveryPreservesOtherLiveOwner(t *testing.T) {
	service, work := workerAuthorityFixture(t)
	recovered := New(service.database, service.now).WithBlobStore(service.blobs)
	if err := recovered.testExecutions().Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	var state, owner string
	var attempt int
	if err := service.database.QueryRowContext(
		t.Context(),
		`SELECT state,COALESCE(worker_id,''),attempt_count FROM jobs WHERE id=?`,
		work.jobID,
	).Scan(
		&state,
		&owner,
		&attempt,
	); err != nil {
		t.Fatal(err)
	}
	if state != "RUNNING" || owner != work.workerID || attempt != work.attempt {
		t.Fatalf("live execution stolen: state=%s owner=%s attempt=%d", state, owner, attempt)
	}
}

func TestImportWorkerClaimPreservesRowsAffectedCause(t *testing.T) {
	for _, table := range []string{"jobs", "import_jobs"} {
		t.Run(
			table,
			func(t *testing.T) {
				service, plan := preparedCommitFixture(t)
				admissions := libraryimportservice.NewImportAdmissions(repository.NewImportAdmissions(service.database), nil, service.tags,
					libraryimportmodel.ImportAdmissionOptions{Now: service.now})
				created, err := admissions.Queue(t.Context(), plan.Request)
				if err != nil {
					t.Fatal(err)
				}
				cause := errors.New("worker claim count unavailable")
				written := int64(0)
				service.database = testsupport.OpenSQLFaultDatabase(
					t,
					service.database,
					testsupport.SQLFaultHooks{
						AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
							if strings.HasPrefix(strings.TrimSpace(query), "UPDATE "+table+" SET state=") && len(args) > 0 &&
								args[0].Value == "RUNNING" {
								count, countErr := result.RowsAffected()
								if countErr != nil {
									return nil, countErr
								}
								written += count
								return creationFaultResult{Result: result, cause: cause}, nil
							}
							return result, nil
						},
					},
				)
				work, err := service.claimImportGroup(t.Context(), created.JobID)
				if !errors.Is(err, cause) || written != 1 || work.jobID != "" {
					t.Fatalf("claim %s cause lost: work=%+v written=%d error=%v", table, work, written, err)
				}
				var state string
				if err := service.database.QueryRowContext(t.Context(), `SELECT state FROM jobs WHERE id=?`, created.JobID).Scan(&state); err != nil {
					t.Fatal(err)
				}
				if state != "QUEUED" {
					t.Fatalf("failed claim left state %s", state)
				}
			},
		)
	}
}

func TestImportWorkerClaimRejectsExpiredExecutionBudget(t *testing.T) {
	service, plan := preparedCommitFixture(t)
	admissions := libraryimportservice.NewImportAdmissions(repository.NewImportAdmissions(service.database), nil, service.tags,
		libraryimportmodel.ImportAdmissionOptions{Now: service.now})
	created, err := admissions.Queue(t.Context(), plan.Request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.database.ExecContext(
		t.Context(),
		`UPDATE jobs SET execution_started_at_ms=1,execution_deadline_at_ms=2 WHERE id=?`,
		created.JobID,
	); err != nil {
		t.Fatal(err)
	}
	work, err := service.claimImportGroup(t.Context(), created.JobID)
	if err == nil || work.jobID != "" {
		t.Fatalf("expired budget claimed: work=%+v error=%v", work, err)
	}
}

func TestImportWorkerFailureAndReleaseSchedulingAreAtomic(t *testing.T) {
	service, work := workerAuthorityFixture(t)
	cause := errors.New("payload scheduling unavailable")
	terminal, release := int64(0), 0
	service.database = testsupport.OpenSQLFaultDatabase(
		t,
		service.database,
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
					terminal += count
				}
				return result, nil
			},
		},
	)
	err := service.testExecutions().Fail(t.Context(), *work.creationIntent(), ErrInvalid)
	if !errors.Is(err, cause) {
		t.Fatalf("release failure cause lost: %v", err)
	}
	var jobState, parentState, payloadState string
	if err := service.database.QueryRowContext(
		t.Context(),
		`SELECT job.state,parent.state,parent.payload_state FROM jobs job JOIN import_jobs parent ON
 parent.id=job.scope_id WHERE job.id=?`,
		work.jobID,
	).Scan(
		&jobState,
		&parentState,
		&payloadState,
	); err != nil {
		t.Fatal(err)
	}
	if terminal != 1 || release != 1 || jobState != "RUNNING" || parentState != "RUNNING" || payloadState != "RETAINED" {
		t.Fatalf(
			"partial failure settlement: terminalWrites=%d releaseAttempts=%d job=%s parent=%s payload=%s",
			terminal,
			release,
			jobState,
			parentState,
			payloadState,
		)
	}
}
