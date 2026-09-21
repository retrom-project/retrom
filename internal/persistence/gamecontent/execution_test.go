package gamecontent

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/service/gamecontent"
	"retrom/internal/testsupport"
)

func executionFixture(t *testing.T, state string) (*sql.DB, gamecontent.Claim) {
	t.Helper()
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), func() time.Time { return time.UnixMilli(100) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	_, err = database.SQL.ExecContext(t.Context(), `INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,
 payload_json,cancellable,state,attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms,version,finished_at_ms,worker_id,cancel_requested_at_ms)
 VALUES('replacement','GAME','game','GAME_CONTENT_REPLACE',?,1,'{"schemaVersion":1,"inputExecutionNo":1}',1,?,0,2,0,0,7,2,?,'owner',?)`,
		strings.Repeat("a", 64), state, finishedTime(state), cancellationTime(state))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("{}"))
	claim := gamecontent.Claim{GameID: "game", JobID: "replacement", WorkerID: "owner", InputDigest: hex.EncodeToString(digest[:]), ExecutionNo: 1, Now: 100, Deadline: 300_100}
	_, err = database.SQL.ExecContext(t.Context(), `INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
 VALUES('replacement',1,'{}',?,0)`, claim.InputDigest)
	if err != nil {
		t.Fatal(err)
	}
	return database.SQL, claim
}

func cancellationTime(state string) *int64 {
	if state != "CANCELLED" && state != "CANCEL_REQUESTED" {
		return nil
	}
	value := int64(7)
	return &value
}

func finishedTime(state string) *int64 {
	if state == "QUEUED" || state == "RUNNING" || state == "CANCEL_REQUESTED" {
		return nil
	}
	value := int64(7)
	return &value
}

func TestReplacementFailureCannotOverwriteCancelledJob(t *testing.T) {
	database, claim := executionFixture(t, "CANCELLED")
	err := New(database).WithWrite(t.Context(), func(scope gamecontent.WriteScope) error {
		changed, err := scope.Jobs.Fail(t.Context(), gamecontent.Outcome{Claim: claim, GameID: "game", Code: "GAME_CONTENT_INPUT_UNAVAILABLE", Retryable: true, Now: 17})
		if changed {
			t.Fatal("cancelled execution accepted failure write")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	assertExecutionState(t, database, "CANCELLED", 2, 0)
}

func TestReplacementClaimAndStartedEventRollbackTogether(t *testing.T) {
	database, claim := executionFixture(t, "QUEUED")
	if _, err := database.ExecContext(t.Context(), `DROP TABLE job_events`); err != nil {
		t.Fatal(err)
	}
	err := New(database).WithWrite(t.Context(), func(scope gamecontent.WriteScope) error {
		_, err := scope.Leases.Claim(t.Context(), claim)
		return err
	})
	if err == nil {
		t.Fatal("claim succeeded without STARTED event")
	}
	var state string
	var attempt, version int
	if err := database.QueryRowContext(t.Context(), `SELECT state,attempt_count,version FROM jobs WHERE id='replacement'`).Scan(&state, &attempt, &version); err != nil {
		t.Fatal(err)
	}
	if state != "QUEUED" || attempt != 0 || version != 2 {
		t.Fatalf("partial claim: %s attempt=%d version=%d", state, attempt, version)
	}
}

func TestReplacementLeaseRejectsExpiredOrReplacedWorkers(t *testing.T) {
	database, claim := executionFixture(t, "QUEUED")
	err := New(database).WithWrite(t.Context(), func(scope gamecontent.WriteScope) error {
		claimed, err := scope.Leases.Claim(t.Context(), claim)
		if err != nil {
			return err
		}
		if !claimed {
			t.Fatal("fresh claim rejected")
		}
		for _, test := range []struct {
			claim gamecontent.Claim
			now   int64
			want  bool
		}{
			{claim, 101, true},
			{claim, 60_100, false},
			{gamecontent.Claim{JobID: claim.JobID, WorkerID: "stale", ExecutionNo: 1}, 101, false},
			{gamecontent.Claim{JobID: claim.JobID, WorkerID: claim.WorkerID, ExecutionNo: 2}, 101, false},
		} {
			current, err := scope.Leases.Current(t.Context(), test.claim, test.now)
			if err != nil {
				return err
			}
			if current != test.want {
				t.Fatalf("ownership accepted stale lease: %+v current=%t", test, current)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReplacementFailureEventCannotPartiallyCommit(t *testing.T) {
	database, claim := executionFixture(t, "RUNNING")
	err := New(database).WithWrite(t.Context(), func(scope gamecontent.WriteScope) error {
		changed, err := scope.Jobs.Fail(t.Context(), gamecontent.Outcome{Claim: claim, GameID: "game", Code: "failed", Retryable: true, Now: 100})
		if err != nil {
			return err
		}
		if !changed {
			t.Fatal("owned failure rejected")
		}
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost late failure: %v", err)
	}
	assertExecutionState(t, database, "RUNNING", 2, 0)
}

func assertExecutionState(t *testing.T, database *sql.DB, wanted string, wantedVersion, wantedEvents int) {
	t.Helper()
	var state string
	var version, events int
	err := database.QueryRowContext(t.Context(), `SELECT state,version,(SELECT count(*) FROM job_events)
 FROM jobs WHERE id='replacement'`).Scan(&state, &version, &events)
	if err != nil {
		t.Fatal(err)
	}
	if state != wanted || version != wantedVersion || events != wantedEvents {
		t.Fatalf("execution = %s v%d / %d events", state, version, events)
	}
}

func TestReplacementClaimRejectsDifferentGameScope(t *testing.T) {
	database, claim := executionFixture(t, "QUEUED")
	if _, err := database.ExecContext(t.Context(), `UPDATE jobs SET scope_id='different-game' WHERE id='replacement'`); err != nil {
		t.Fatal(err)
	}
	err := New(database).WithWrite(t.Context(), func(scope gamecontent.WriteScope) error {
		claimed, err := scope.Leases.Claim(t.Context(), claim)
		if claimed {
			t.Fatal("worker claimed a different game")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	assertExecutionState(t, database, "QUEUED", 2, 0)
}
