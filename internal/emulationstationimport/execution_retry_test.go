package emulationstationimport

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"testing"
	"time"

	"retrom/internal/serversource"
	"retrom/internal/testassert"
)

func TestFrozenOpenErrorsSeparateDriftFromTransientIO(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "root unavailable", err: serversource.ErrRootUnavailable, want: serversource.ErrRootUnavailable},
		{name: "source removed", err: fs.ErrNotExist, want: ErrSourceChanged},
		{name: "source path drift", err: serversource.ErrPathInvalid, want: ErrSourceChanged},
		{name: "unclassified IO", err: errors.New("input/output error"), want: serversource.ErrRootUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifyFrozenOpenError(test.err)
			testassert.Truef(t, errors.Is(got, test.want), "error = %v, want %v", got, test.want)
		})
	}
}

func TestAutomaticRetryDelaysAreBounded(t *testing.T) {
	wants := []time.Duration{time.Second, 5 * time.Second, 30 * time.Second, 120 * time.Second}
	for index, want := range wants {
		attempt := int64(index + 1)
		testassert.Falsef(t, automaticRetryDelay(attempt) != want,
			"attempt %d delay = %s, want %s", attempt, automaticRetryDelay(attempt), want)
	}
}

func TestTransientScanFailureSchedulesRetryWithFrozenDeadline(t *testing.T) {
	fixture := newLifecycleFixture(t)
	created, err := fixture.service.Create(
		fixture.context, CreateRequest{RootID: "games", SourceRelativePath: ""}, fixture.userID,
	)
	testassert.False(t, err != nil, err)
	unit, found := fixture.service.claim(fixture.context)
	testassert.True(t, found, "scan work was not claimable")
	frozenDeadline := unit.DeadlineAtMS

	fixture.service.fail(fixture.context, unit, "INTERNAL_ERROR", true)

	now := fixture.now.UnixMilli()
	var jobState, aggregateState, phase string
	var attempt, availableAt, persistedDeadline, retryEvents int64
	var eventAttempt, eventRetryAt, eventRetryable int64
	var eventCode string
	var errorCode sql.NullString
	testassert.False(t, fixture.database.QueryRowContext(fixture.context, `
SELECT job.state,job.attempt_count,job.available_at_ms,job.execution_deadline_at_ms,job.error_code,
 import.state,import.phase,
	 (SELECT count(*) FROM job_events event WHERE event.job_id=job.id AND event.event_type='RETRY_SCHEDULED'),
	 (SELECT json_extract(event.data_json,'$.attempt') FROM job_events event
	  WHERE event.job_id=job.id AND event.event_type='RETRY_SCHEDULED'),
	 (SELECT json_extract(event.data_json,'$.retryAtMs') FROM job_events event
	  WHERE event.job_id=job.id AND event.event_type='RETRY_SCHEDULED'),
	 (SELECT json_extract(event.data_json,'$.errorCode') FROM job_events event
	  WHERE event.job_id=job.id AND event.event_type='RETRY_SCHEDULED'),
	 (SELECT json_extract(event.data_json,'$.errorRetryable') FROM job_events event
	  WHERE event.job_id=job.id AND event.event_type='RETRY_SCHEDULED')
FROM jobs job JOIN emulationstation_imports import ON import.id=job.scope_id
WHERE job.id=?`, unit.JobID).Scan(
		&jobState, &attempt, &availableAt, &persistedDeadline, &errorCode,
		&aggregateState, &phase, &retryEvents,
		&eventAttempt, &eventRetryAt, &eventCode, &eventRetryable,
	) != nil, "read scheduled retry")
	testassert.Falsef(t, testassert.Any(
		func() bool { return jobState != "QUEUED" },
		func() bool { return aggregateState != "SCANNING" || phase != "DISCOVERING_GAMELISTS" },
		func() bool { return attempt != 1 || availableAt != now+time.Second.Milliseconds() },
		func() bool { return persistedDeadline != frozenDeadline },
		func() bool { return errorCode.Valid || retryEvents != 1 },
		func() bool { return eventAttempt != 1 || eventRetryAt != availableAt },
		func() bool { return eventCode != "INTERNAL_ERROR" || eventRetryable != 1 },
	), "retry = job:%s aggregate:%s phase:%s attempt:%d available:%d deadline:%d events:%d",
		jobState, aggregateState, phase, attempt, availableAt, persistedDeadline, retryEvents)

	_, claimedEarly := fixture.service.claim(fixture.context)
	testassert.False(t, claimedEarly, "retry was claimable before its backoff")
	*fixture.now = fixture.now.Add(time.Second)
	retried, claimed := fixture.service.claim(fixture.context)
	testassert.Truef(t, claimed, "retry was not claimable after its backoff: %#v", retried)
	testassert.Falsef(t, retried.Attempt != 2 || retried.DeadlineAtMS != frozenDeadline,
		"retried work = %#v, frozen deadline = %d", retried, frozenDeadline)
	current, err := fixture.service.Get(fixture.context, created.ID)
	testassert.Falsef(t, err != nil || current.State != "SCANNING",
		"current scan = %#v, error = %v", current, err)
}

func TestUnavailableRootIsRetriedAsTransientIO(t *testing.T) {
	fixture := newLifecycleFixture(t)
	_, err := fixture.service.Create(
		fixture.context, CreateRequest{RootID: "games", SourceRelativePath: ""}, fixture.userID,
	)
	testassert.False(t, err != nil, err)
	unit, found := fixture.service.claim(fixture.context)
	testassert.True(t, found, "scan work was not claimable")
	testassert.False(t, os.RemoveAll(fixture.source) != nil, "remove temporary source root")

	fixture.service.execute(fixture.context, unit)

	var jobState, eventCode string
	testassert.False(t, fixture.database.QueryRowContext(fixture.context, `
SELECT job.state,json_extract(event.data_json,'$.errorCode')
FROM jobs job JOIN job_events event ON event.job_id=job.id
WHERE job.id=? AND event.event_type='RETRY_SCHEDULED'`, unit.JobID).Scan(
		&jobState, &eventCode,
	) != nil, "read unavailable-root retry")
	testassert.Falsef(t, jobState != "QUEUED" || eventCode != "SERVER_IMPORT_ROOT_UNAVAILABLE",
		"root failure = state:%s eventCode:%s", jobState, eventCode)
}

func TestAutomaticRetryExhaustionBecomesTerminal(t *testing.T) {
	fixture := newLifecycleFixture(t)
	created, err := fixture.service.Create(
		fixture.context, CreateRequest{RootID: "games", SourceRelativePath: ""}, fixture.userID,
	)
	testassert.False(t, err != nil, err)
	unit, found := fixture.service.claim(fixture.context)
	testassert.True(t, found, "scan work was not claimable")
	mustExecEmulationStationTest(t, fixture.database, `
UPDATE jobs SET attempt_count=max_attempts WHERE id=?`, unit.JobID)

	fixture.service.fail(fixture.context, unit, "INTERNAL_ERROR", true)

	assertTerminalExecutionCode(
		t, fixture, created.ID, unit.JobID, "EMULATIONSTATION_WORKER_ATTEMPTS_EXHAUSTED",
	)
}

func TestDeadlineFailurePersistsStableTimeoutWithFreshContext(t *testing.T) {
	fixture := newLifecycleFixture(t)
	created, err := fixture.service.Create(
		fixture.context, CreateRequest{RootID: "games", SourceRelativePath: ""}, fixture.userID,
	)
	testassert.False(t, err != nil, err)
	unit, found := fixture.service.claim(fixture.context)
	testassert.True(t, found, "scan work was not claimable")
	unit.DeadlineAtMS = fixture.now.UnixMilli()
	deadlineContext, cancel := context.WithDeadline(fixture.context, fixture.now.Add(-time.Second))
	defer cancel()

	fixture.service.fail(deadlineContext, unit, "INTERNAL_ERROR", true)
	fixture.service.fail(deadlineContext, unit, "INTERNAL_ERROR", true)

	assertTerminalExecutionCode(t, fixture, created.ID, unit.JobID, "EMULATIONSTATION_EXECUTION_TIMEOUT")
}

func assertTerminalExecutionCode(
	t *testing.T,
	fixture lifecycleFixture,
	importID, jobID, want string,
) {
	t.Helper()
	var jobState, jobCode, aggregateState, aggregateCode string
	var retryable, retryEvents, failedEvents int64
	testassert.False(t, fixture.database.QueryRowContext(fixture.context, `
SELECT job.state,job.error_code,job.error_retryable,import.state,import.last_error_code,
	 (SELECT count(*) FROM job_events event WHERE event.job_id=job.id AND event.event_type='RETRY_SCHEDULED')
	,(SELECT count(*) FROM job_events event WHERE event.job_id=job.id AND event.event_type='FAILED')
FROM jobs job JOIN emulationstation_imports import ON import.id=job.scope_id
WHERE import.id=? AND job.id=?`, importID, jobID).Scan(
		&jobState, &jobCode, &retryable, &aggregateState, &aggregateCode, &retryEvents, &failedEvents,
	) != nil, "read terminal execution")
	testassert.Falsef(t, testassert.Any(
		func() bool { return jobState != "FAILED" || aggregateState != "FAILED" },
		func() bool { return jobCode != want || aggregateCode != want },
		func() bool { return retryable != 0 || retryEvents != 0 || failedEvents != 1 },
	), "terminal = job:%s/%s aggregate:%s/%s retryable:%d retryEvents:%d",
		jobState, jobCode, aggregateState, aggregateCode, retryable, retryEvents)
}
