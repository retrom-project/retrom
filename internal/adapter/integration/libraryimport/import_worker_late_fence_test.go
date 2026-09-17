//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	application "retrom/internal/model/libraryimport"
	"retrom/internal/testkit/testsupport"
)

func TestImportWorkerLateLeaseFailureRollsBackTerminalAndRelease(t *testing.T) {
	service, work := workerAuthorityFixture(t)
	var clock atomic.Int64
	clock.Store(service.now().UnixMilli())
	service.now = func() time.Time { return time.UnixMilli(clock.Load()) }
	source := service.database
	before := creationEffectCounts(t, source)
	writes := int64(0)
	service.database = testsupport.OpenSQLFaultDatabase(
		t,
		source,
		testsupport.SQLFaultHooks{
			AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
				if strings.HasPrefix(strings.TrimSpace(query), "UPDATE jobs SET state=") && len(args) > 0 &&
					args[0].Value == "FAILED" {
					count, err := result.RowsAffected()
					if err != nil {
						return nil, err
					}
					writes += count
					clock.Add(2 * application.ImportExecutionLease.Milliseconds())
				}
				return result, nil
			},
		},
	)
	err := service.testExecutions().Fail(t.Context(), *work.creationIntent(), ErrInvalid)
	if !errors.Is(err, ErrVersionConflict) || writes != 1 {
		t.Fatalf("late lease fence: writes=%d error=%v", writes, err)
	}
	assertCreationEffectsUnchanged(t, source, before)
	var state string
	if err := source.QueryRowContext(t.Context(), `SELECT state FROM jobs WHERE id=?`, work.jobID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "RUNNING" {
		t.Fatalf("expired lease persisted terminal state %s", state)
	}
}

func TestImportWorkerRecoveryRetainsAlreadyCreatedResults(t *testing.T) {
	service, plan := preparedCommitFixture(t)
	result, err := service.importCreations().CommitPrepared(t.Context(), plan, application.ImportCreationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	before := creationEffectCounts(t, service.database)
	id := result.Created.JobID
	if _, err := service.database.ExecContext(
		t.Context(),
		`
UPDATE jobs SET state='RUNNING',finished_at_ms=NULL,worker_id='interrupted',leased_until_ms=1,
 execution_started_at_ms=1,
execution_deadline_at_ms=?,version=version+1 WHERE id=?`,
		service.now().Add(time.Hour).UnixMilli(),
		id,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := service.database.ExecContext(
		t.Context(),
		`UPDATE import_jobs SET state='RUNNING',version=version+1 WHERE id=?`,
		result.Created.ImportJobID,
	); err != nil {
		t.Fatal(err)
	}
	if err := service.testExecutions().Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	for table, count := range creationEffectCounts(t, service.database) {
		if table != "job_events" && count != before[table] {
			t.Fatalf("recovery changed %s rows: before=%d after=%d", table, before[table], count)
		}
	}
	var job, parent string
	if err := service.database.QueryRowContext(
		t.Context(),
		`SELECT job.state,parent.state FROM jobs job JOIN import_jobs parent ON parent.id=job.scope_id WHERE job.id=?`,
		id,
	).Scan(
		&job,
		&parent,
	); err != nil {
		t.Fatal(err)
	}
	if job != "SUCCEEDED" || parent != "REVIEW_PENDING" {
		t.Fatalf("recovered created result=%s/%s", job, parent)
	}
	work, found, err := service.testExecutions().Claim(t.Context(), id)
	if err != nil || found || work.Execution.JobID != "" {
		t.Fatalf("completed result was reclaimed: %+v %t %v", work, found, err)
	}
}

func TestImportWorkerRecoveryRetainsResolvedFilesWithoutItems(t *testing.T) {
	service, plan := preparedCommitFixture(t)
	plan.Groups = nil
	for index := range plan.Dispositions {
		plan.Dispositions[index].Disposition = "REJECTED"
		plan.Dispositions[index].Reason = "UNSUPPORTED_CONTENT_FORMAT"
	}
	result, err := service.importCreations().CommitPrepared(t.Context(), plan, application.ImportCreationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Created.ItemCount != 0 || result.Created.State != "PARTIAL_FAILURE" {
		t.Fatalf("rejected creation=%+v", result)
	}
	if _, err := service.database.ExecContext(
		t.Context(),
		`UPDATE jobs SET state='RUNNING',finished_at_ms=NULL,worker_id='interrupted',leased_until_ms=1,
 version=version+1 WHERE id=?`,
		result.Created.JobID,
	); err != nil {
		t.Fatal(err)
	}
	if err := service.testExecutions().Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	var job, parent string
	var rejected int
	if err := service.database.QueryRowContext(
		t.Context(),
		`SELECT job.state,parent.state,parent.rejected_file_count FROM jobs job JOIN import_jobs parent ON
 parent.id=job.scope_id WHERE job.id=?`,
		result.Created.JobID,
	).Scan(
		&job,
		&parent,
		&rejected,
	); err != nil {
		t.Fatal(err)
	}
	if job != "SUCCEEDED" || parent != "PARTIAL_FAILURE" || rejected != len(plan.Dispositions) {
		t.Fatalf("rejected evidence recovery=%s/%s/%d", job, parent, rejected)
	}
}
