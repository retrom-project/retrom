//go:build integration

package launch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"retrom/internal/testkit/testsupport"

	persistence "retrom/internal/repo/launch"
	application "retrom/internal/service/launch"
)

type validationWorkerFixture struct {
	service  *Service
	database *sql.DB
	gameID   string
	now      func() time.Time
}

func validationWorkerSQL(t *testing.T, database *sql.DB, statement string, args ...any) {
	t.Helper()
	if _, err := database.ExecContext(t.Context(), statement, args...); err != nil {
		t.Fatal(err)
	}
}

type validationWorkerJob struct {
	State, ErrorCode, WorkerID            string
	Version, Attempt, Execution, Events   int64
	Started, Deadline, Lease, CancelledAt sql.NullInt64
}

func queuedValidationWorker(t *testing.T) (validationWorkerFixture, string) {
	t.Helper()
	now := func() time.Time { return time.UnixMilli(1_786_000_000_000) }
	source := newScummVMFixtureAt(t, []string{"Two"}, now)
	approved, err := source.importer.Approve(t.Context(), source.itemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := source.service.EnsureVariantForMove(t.Context(), approved.GameID, "scummvm")
	if err != nil || pending.JobID == "" {
		t.Fatalf("queue real variant: %+v %v", pending, err)
	}
	return validationWorkerFixture{service: source.service, database: source.database, gameID: approved.GameID, now: now}, pending.JobID
}

func readValidationWorkerJob(t *testing.T, database *sql.DB, id string) validationWorkerJob {
	t.Helper()
	var job validationWorkerJob
	err := database.QueryRowContext(t.Context(), `SELECT state,COALESCE(error_code,''),COALESCE(worker_id,''),version,
attempt_count,execution_no,(SELECT count(*) FROM job_events WHERE job_id=jobs.id),execution_started_at_ms,
execution_deadline_at_ms,leased_until_ms,cancel_requested_at_ms FROM jobs WHERE id=?`, id).Scan(
		&job.State, &job.ErrorCode, &job.WorkerID, &job.Version, &job.Attempt, &job.Execution, &job.Events,
		&job.Started, &job.Deadline, &job.Lease, &job.CancelledAt)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func TestValidationWorkerClaimRollsBackWhenStartedEventFails(t *testing.T) {
	fixture, id := queuedValidationWorker(t)
	before := readValidationWorkerJob(t, fixture.database, id)
	hits := injectValidationEventFailure(t, fixture, id, "STARTED")
	fixture.service.ResumeValidationJob(t.Context(), id)
	if *hits != 1 {
		t.Fatalf("STARTED event fault hits: %d", *hits)
	}
	if after := readValidationWorkerJob(t, fixture.database, id); !reflect.DeepEqual(before, after) {
		t.Fatalf("failed claim advanced Job before=%+v after=%+v", before, after)
	}
}

func TestValidationWorkerGameChangeClosesWithCancellationMetadata(t *testing.T) {
	fixture, id := queuedValidationWorker(t)
	validationWorkerSQL(t, fixture.database, `UPDATE games SET version=version+1 WHERE id=?`, fixture.gameID)
	fixture.service.ResumeValidationJob(t.Context(), id)
	job := readValidationWorkerJob(t, fixture.database, id)
	if job.State != "CANCELLED" || job.ErrorCode != "GAME_STATE_CHANGED" || !job.CancelledAt.Valid || job.CancelledAt.Int64 != fixture.now().UnixMilli() {
		t.Fatalf("changed game did not settle cancellation: %+v", job)
	}
	var events int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM job_events WHERE job_id=? AND event_type='CANCELLED'`, id).Scan(&events); err != nil || events != 1 {
		t.Fatalf("cancel events=%d error=%v", events, err)
	}
}

func TestValidationWorkerFutureJobCannotBeClaimed(t *testing.T) {
	fixture, id := queuedValidationWorker(t)
	validationWorkerSQL(t, fixture.database, `UPDATE jobs SET available_at_ms=? WHERE id=?`, fixture.now().UnixMilli()+1, id)
	before := readValidationWorkerJob(t, fixture.database, id)
	fixture.service.ResumeValidationJob(t.Context(), id)
	if after := readValidationWorkerJob(t, fixture.database, id); !reflect.DeepEqual(before, after) {
		t.Fatalf("future Job claimed before=%+v after=%+v", before, after)
	}
}

func TestValidationWorkerRecoveryRetainsExecutionDeadline(t *testing.T) {
	fixture, id := queuedValidationWorker(t)
	now := fixture.now().UnixMilli()
	validationWorkerSQL(t, fixture.database, `UPDATE jobs SET state='RUNNING',attempt_count=1,execution_started_at_ms=?,
execution_deadline_at_ms=?,leased_until_ms=?,heartbeat_at_ms=?,worker_id='old-worker' WHERE id=?`,
		now-120000, now+1000, now-1, now-60001, id)
	before := readValidationWorkerJob(t, fixture.database, id)
	if _, err := fixture.service.validationWorker().Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	after := readValidationWorkerJob(t, fixture.database, id)
	if after.State != "QUEUED" || after.Execution != before.Execution || after.Started != before.Started || after.Deadline != before.Deadline {
		t.Fatalf("recovery reset execution budget before=%+v after=%+v", before, after)
	}
}

func TestValidationWorkerOldAttemptCannotSettleReplacement(t *testing.T) {
	fixture, id := queuedValidationWorker(t)
	calls := 0
	var replacement validationWorkerJob
	fixture.service.now = func() time.Time {
		calls++
		if calls == 2 {
			validationWorkerSQL(t, fixture.database, `UPDATE jobs SET worker_id='replacement',attempt_count=2,version=version+1 WHERE id=?`, id)
			replacement = readValidationWorkerJob(t, fixture.database, id)
		}
		return fixture.now()
	}
	fixture.service.ResumeValidationJob(t.Context(), id)
	if after := readValidationWorkerJob(t, fixture.database, id); !reflect.DeepEqual(replacement, after) {
		t.Fatalf("old attempt settled replacement before=%+v after=%+v", replacement, after)
	}
}

func TestValidationWorkerFailureEventIsAtomic(t *testing.T) {
	fixture, id := queuedValidationWorker(t)
	factsHits, eventHits := 0, 0
	fault := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
			if strings.Contains(query, "FROM games game JOIN platform_instances") && len(args) == 2 && args[1].Value == fixture.gameID {
				factsHits++
				return errors.New("facts unavailable")
			}
			return nil
		},
		BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
			if validationEventMatches(query, args, id, "FAILED") {
				eventHits++
				return errors.New("FAILED event unavailable")
			}
			return nil
		},
	})
	fixture.service.database = fault
	fixture.service.ResumeValidationJob(t.Context(), id)
	after := readValidationWorkerJob(t, fixture.database, id)
	if factsHits != 1 || eventHits != 1 || after.State != "RUNNING" || after.Version != 2 || after.Events != 2 || after.ErrorCode != "" {
		t.Fatalf("failure event atomicity: facts=%d event=%d result=%+v", factsHits, eventHits, after)
	}
}

func assertValidationRejectsRetiredBIOS(t *testing.T, ctx context.Context, database *sql.DB, selected application.ProductSnapshot, variantID string) {
	t.Helper()
	inputs, err := application.ProductValidationInputs(selected, variantID)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := persistence.NewValidationWorker(database).Facts(ctx, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.EvaluateValidation(inputs, facts); !errors.Is(err, application.ErrValidationGameChanged) {
		t.Fatalf("late validation revived retired BIOS: %v", err)
	}
}

func TestValidationWorkerRecoveryExhaustionWritesExactlyOneEvent(t *testing.T) {
	fixture, id := queuedValidationWorker(t)
	now := fixture.now().UnixMilli()
	validationWorkerSQL(t, fixture.database, `UPDATE jobs SET state='RUNNING',attempt_count=2,execution_started_at_ms=?,execution_deadline_at_ms=?,leased_until_ms=?,worker_id='expired-owner' WHERE id=?`, now-10000, now+1000, now-1, id)
	for range 2 {
		if _, err := fixture.service.validationWorker().Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	after := readValidationWorkerJob(t, fixture.database, id)
	if after.State != "FAILED" || after.Attempt != 2 || after.Execution != 1 || after.Events != 2 || after.Deadline.Int64 != now+1000 {
		t.Fatalf("exhausted recovery changed execution: %+v", after)
	}
}

func TestValidationWorkerRecoveryFailureEventRollsBack(t *testing.T) {
	fixture, id := queuedValidationWorker(t)
	now := fixture.now().UnixMilli()
	validationWorkerSQL(t, fixture.database, `UPDATE jobs SET state='RUNNING',attempt_count=2,execution_started_at_ms=?,execution_deadline_at_ms=?,leased_until_ms=?,worker_id='expired-owner' WHERE id=?`, now-10000, now+1000, now-1, id)
	before := readValidationWorkerJob(t, fixture.database, id)
	hits := injectValidationEventFailure(t, fixture, id, "FAILED")
	if _, err := fixture.service.validationWorker().Recover(t.Context()); err == nil {
		t.Fatal("recovery event failure swallowed")
	}
	if *hits != 1 {
		t.Fatalf("FAILED recovery event fault hits: %d", *hits)
	}
	if after := readValidationWorkerJob(t, fixture.database, id); !reflect.DeepEqual(before, after) {
		t.Fatalf("partial recovery: before=%+v after=%+v", before, after)
	}
}

func injectValidationEventFailure(t *testing.T, fixture validationWorkerFixture, id, event string) *int {
	t.Helper()
	hits := 0
	fault := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
		if validationEventMatches(query, args, id, event) {
			hits++
			return errors.New("validation event unavailable")
		}
		return nil
	}})
	fixture.service.database = fault
	return &hits
}

func validationEventMatches(query string, args []driver.NamedValue, id, event string) bool {
	return strings.Contains(query, "INSERT INTO job_events") && len(args) == 6 && args[0].Value == id && args[3].Value == event
}

func TestValidationWorkerPublicCloseCancelsAndJoinsActiveAttempt(t *testing.T) {
	fixture, id := queuedValidationWorker(t)
	entered := make(chan struct{})
	fault := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{BeforeQuery: func(ctx context.Context, query string, args []driver.NamedValue) error {
		if strings.Contains(query, "FROM games game JOIN platform_instances") && len(args) == 2 && args[1].Value == fixture.gameID {
			close(entered)
			<-ctx.Done()
			return context.Cause(ctx)
		}
		return nil
	}})
	fixture.service.database = fault
	finished := make(chan struct{})
	go func() { fixture.service.ResumeValidationJob(t.Context(), id); close(finished) }()
	select {
	case <-entered:
	case <-finished:
		t.Fatal("worker exited before blocked facts")
	case <-time.After(time.Second):
		t.Fatal("worker did not reach facts")
	}
	fixture.service.Close()
	<-finished
	after := readValidationWorkerJob(t, fixture.database, id)
	if after.State != "FAILED" || after.Attempt != 1 || after.Events != 3 {
		t.Fatalf("Close left worker unsettled: %+v", after)
	}
}

func TestValidationWorkerPublicCloseRejectsNewAttempt(t *testing.T) {
	fixture, id := queuedValidationWorker(t)
	before := readValidationWorkerJob(t, fixture.database, id)
	fixture.service.Close()
	fixture.service.ResumeValidationJob(t.Context(), id)
	if after := readValidationWorkerJob(t, fixture.database, id); !reflect.DeepEqual(before, after) {
		t.Fatalf("closed worker claimed new work: %+v", after)
	}
}

func TestValidationWorkerTerminalEventRetainsPublicContract(t *testing.T) {
	fixture, id := queuedValidationWorker(t)
	fixture.service.ResumeValidationJob(t.Context(), id)
	var raw, scopeID string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT data_json,scope_id FROM job_events WHERE job_id=? AND event_type='SUCCEEDED'`, id).Scan(&raw, &scopeID); err != nil {
		t.Fatal(err)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 2 || payload["code"] != "READY" || payload["gameVariantId"] != scopeID {
		t.Fatalf("success event changed: %s", raw)
	}
	var errorCode sql.NullString
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT error_code FROM jobs WHERE id=?`, id).Scan(&errorCode); err != nil || errorCode.Valid {
		t.Fatalf("success error: %+v %v", errorCode, err)
	}
}

func TestValidationWorkerCorruptSnapshotPreservesJSONCause(t *testing.T) {
	for _, kind := range []string{"syntax", "type"} {
		t.Run(kind, func(t *testing.T) {
			fixture, id := queuedValidationWorker(t)
			corruptValidationWorkerSnapshot(t, fixture, id, kind)
			before := validationWorkerVariantState(t, fixture)
			err := fixture.service.validationWorker().Run(t.Context(), id)
			assertValidationJSONCause(t, err, kind)
			assertValidationVariantUnchanged(t, fixture, before)
			after := readValidationWorkerJob(t, fixture.database, id)
			if after.State != "FAILED" || after.ErrorCode != "LAUNCH_CORE_VALIDATION_UNAVAILABLE" || after.Attempt != 1 || after.Events != 3 {
				t.Fatalf("malformed execution did not close: %+v", after)
			}
			var retryable bool
			var event string
			if err := fixture.database.QueryRowContext(t.Context(), `SELECT error_retryable,(SELECT data_json FROM job_events WHERE job_id=jobs.id AND event_type='FAILED') FROM jobs WHERE id=?`, id).Scan(&retryable, &event); err != nil {
				t.Fatal(err)
			}
			if retryable || event != `{"code":"LAUNCH_CORE_VALIDATION_UNAVAILABLE"}` {
				t.Fatalf("malformed terminal contract: retryable=%v event=%s", retryable, event)
			}
		})
	}
}

func TestValidationWorkerCorruptSnapshotFailureIsAtomic(t *testing.T) {
	fixture, id := queuedValidationWorker(t)
	corruptValidationWorkerSnapshot(t, fixture, id, "syntax")
	before := validationWorkerVariantState(t, fixture)
	hits := injectValidationEventFailure(t, fixture, id, "FAILED")
	err := fixture.service.validationWorker().Run(t.Context(), id)
	assertValidationJSONCause(t, err, "syntax")
	assertValidationVariantUnchanged(t, fixture, before)
	after := readValidationWorkerJob(t, fixture.database, id)
	if *hits != 1 || after.State != "RUNNING" || after.Version != 2 || after.Events != 2 || after.ErrorCode != "" {
		t.Fatalf("partial malformed terminal: hits=%d result=%+v", *hits, after)
	}
}

func corruptValidationWorkerSnapshot(t *testing.T, fixture validationWorkerFixture, id, kind string) {
	t.Helper()
	var raw string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT input_json FROM job_input_snapshots WHERE job_id=? AND execution_no=1`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if kind == "syntax" {
		raw = raw[:len(raw)-1]
	} else {
		raw = strings.Replace(raw, `"schemaVersion":1`, `"schemaVersion":"corrupt"`, 1)
	}
	digest := sha256.Sum256([]byte(raw))
	validationWorkerSQL(t, fixture.database, `UPDATE job_input_snapshots SET input_json=?,input_digest=? WHERE job_id=? AND execution_no=1`, raw, hex.EncodeToString(digest[:]), id)
}

func assertValidationJSONCause(t *testing.T, err error, kind string) {
	t.Helper()
	if !errors.Is(err, application.ErrValidationInput) {
		t.Fatalf("lost stable malformed-input category: %v", err)
	}
	var syntax *json.SyntaxError
	var fieldType *json.UnmarshalTypeError
	if kind == "syntax" && !errors.As(err, &syntax) || kind == "type" && !errors.As(err, &fieldType) {
		t.Fatalf("lost %s JSON cause: %v", kind, err)
	}
}

type validationVariantState struct {
	Version, Files           int64
	Status, Code, Dependency string
}

func validationWorkerVariantState(t *testing.T, fixture validationWorkerFixture) validationVariantState {
	t.Helper()
	var state validationVariantState
	err := fixture.database.QueryRowContext(t.Context(), `SELECT version,status,compatibility_code,dependency_snapshot_json,(SELECT count(*) FROM variant_files WHERE game_variant_id=game_variants.id) FROM game_variants WHERE game_id=?`, fixture.gameID).Scan(&state.Version, &state.Status, &state.Code, &state.Dependency, &state.Files)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func assertValidationVariantUnchanged(t *testing.T, fixture validationWorkerFixture, before validationVariantState) {
	t.Helper()
	if after := validationWorkerVariantState(t, fixture); after != before {
		t.Fatalf("malformed snapshot wrote variant: before=%+v after=%+v", before, after)
	}
}
