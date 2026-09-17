//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	composition "retrom/internal/bootstrap/composition/libraryimport"
	jobsmodel "retrom/internal/model/jobs"
	libraryimportmodel "retrom/internal/model/libraryimport"
	jobpersistence "retrom/internal/repo/jobs"
	repository "retrom/internal/repo/libraryimport"
	jobsservice "retrom/internal/service/jobs"
	libraryimportservice "retrom/internal/service/libraryimport"
	"retrom/internal/testkit/testsupport"
)

func queuedCancellationFixture(t *testing.T) (*Service, Created) {
	t.Helper()
	service, plan := preparedCommitFixture(t)
	admission := libraryimportservice.NewImportAdmissions(
		repository.NewImportAdmissions(service.database),
		nil,
		service.tags,
		libraryimportmodel.ImportAdmissionOptions{Now: service.now},
	)
	created, err := admission.Queue(t.Context(), plan.Request)
	if err != nil {
		t.Fatal(err)
	}
	return service, created
}

func TestImportWorkerDomainCancelRollsBackFailedPayloadScheduling(t *testing.T) {
	service, created := queuedCancellationFixture(t)
	source := service.database
	cause := errors.New("cancel payload scheduling unavailable")
	writes, attempts := int64(0), 0
	fault := testsupport.OpenSQLFaultDatabase(
		t,
		source,
		testsupport.SQLFaultHooks{
			BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
				if strings.HasPrefix(strings.TrimSpace(query), "INSERT INTO jobs(") && strings.Contains(query, "'PAYLOAD_RELEASE'") {
					attempts++
					return cause
				}
				return nil
			},
			AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
				if strings.HasPrefix(strings.TrimSpace(query), "UPDATE jobs SET state=") && len(args) > 0 &&
					args[0].Value == "CANCELLED" {
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
	handler := composition.WithJobCancellation(jobsservice.New(jobpersistence.New(fault), service.now), fault, service.now)
	result, pending, err := handler.Cancel(t.Context(), created.JobID, 1, "operator request")
	if !errors.Is(err, cause) || result != (jobsmodel.Result{}) || pending || writes != 1 || attempts != 1 {
		t.Fatalf(
			"cancel atomicity: result=%+v pending=%t writes=%d attempts=%d error=%v",
			result,
			pending,
			writes,
			attempts,
			err,
		)
	}
	assertImportCancellationState(t, service, created, "QUEUED", "QUEUED", "RETAINED", 1)
	handler = composition.WithJobCancellation(jobsservice.New(jobpersistence.New(source), service.now), source, service.now)
	result, pending, err = handler.Cancel(t.Context(), created.JobID, 1, "operator request")
	if err != nil || pending || result.State != "CANCELLED" || result.Version != 2 {
		t.Fatalf("cancel retry=%+v pending=%t error=%v", result, pending, err)
	}
	assertImportCancellationState(t, service, created, "CANCELLED", "CANCELLED", "RELEASING", 2)
}

func TestImportWorkerDomainCancelKeepsOwnerUntilExecutionStops(t *testing.T) {
	service, created := queuedCancellationFixture(t)
	executions := service.testExecutions()
	work, found, err := executions.Claim(t.Context(), created.JobID)
	if err != nil || !found {
		t.Fatalf("claim found=%t error=%v", found, err)
	}
	result, err := executions.CancelJob(t.Context(), libraryimportmodel.ImportJobCancellation{
		JobID: created.JobID, ImportID: created.ImportJobID, ExpectedVersion: 2, Reason: "operator stop",
	})
	if err != nil || !result.Pending || result.State != "CANCEL_REQUESTED" {
		t.Fatalf("pending cancel=%+v error=%v", result, err)
	}
	assertImportCancellationState(t, service, created, "CANCEL_REQUESTED", "CANCEL_REQUESTED", "RETAINED", 3)
	var owner string
	if err := service.database.QueryRowContext(t.Context(), `SELECT worker_id FROM jobs WHERE id=?`, created.JobID).Scan(
		&owner,
	); err != nil {
		t.Fatal(err)
	}
	if owner != work.Execution.WorkerID {
		t.Fatal("pending cancellation detached the executing owner")
	}
	if err := executions.Fail(t.Context(), work.Execution, context.Canceled); err != nil {
		t.Fatal(err)
	}
	assertImportCancellationState(t, service, created, "CANCELLED", "CANCELLED", "RELEASING", 4)
}

func assertImportCancellationState(
	t *testing.T,
	service *Service,
	created Created,
	job, parent, payload string,
	version int64,
) {
	t.Helper()
	var actualJob, actualParent, actualPayload string
	var actualVersion int64
	if err := service.database.QueryRowContext(
		t.Context(),
		`
SELECT job.state,parent.state,parent.payload_state,job.version FROM jobs job JOIN import_jobs parent
 ON parent.id=job.scope_id WHERE job.id=?`,
		created.JobID,
	).
		Scan(
			&actualJob,
			&actualParent,
			&actualPayload,
			&actualVersion,
		); err != nil {
		t.Fatal(err)
	}
	if actualJob != job || actualParent != parent || actualPayload != payload || actualVersion != version {
		t.Fatalf(
			"cancel state=%s/%s/%s/v%d expected=%s/%s/%s/v%d",
			actualJob,
			actualParent,
			actualPayload,
			actualVersion,
			job,
			parent,
			payload,
			version,
		)
	}
}

func TestImportWorkerQueuedCancellationPreservesAbsentExecutionTimes(t *testing.T) {
	service, created := queuedCancellationFixture(t)
	_, err := service.testExecutions().CancelJob(t.Context(), libraryimportmodel.ImportJobCancellation{
		JobID: created.JobID, ImportID: created.ImportJobID, ExpectedVersion: 1, Reason: "operator stop",
	})
	if err != nil {
		t.Fatal(err)
	}
	var absent bool
	if err := service.database.QueryRowContext(
		t.Context(),
		`SELECT execution_started_at_ms IS NULL AND execution_deadline_at_ms IS NULL FROM jobs WHERE id=?`,
		created.JobID,
	).Scan(
		&absent,
	); err != nil {
		t.Fatal(err)
	}
	if !absent {
		t.Fatal("cancelled unclaimed job gained epoch execution timestamps")
	}
}
