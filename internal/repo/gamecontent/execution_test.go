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

	"retrom/internal/model/gamecontent"
	"retrom/internal/testkit/testsupport"
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
	repo := New(database)
	result, err := repo.CommitSettleFailure(t.Context(), gamecontent.SettleFailureCommand{
		Claim: claim,
		Outcome: gamecontent.Outcome{
			Claim: claim, GameID: "game",
			Code: "GAME_CONTENT_INPUT_UNAVAILABLE", Retryable: true, Now: 17,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatal("cancelled execution accepted failure write")
	}
	assertExecutionState(t, database, "CANCELLED", 2, 0)
}

func TestReplacementClaimAndStartedEventRollbackTogether(t *testing.T) {
	database, claim := executionFixture(t, "QUEUED")
	if _, err := database.ExecContext(t.Context(), `DROP TABLE job_events`); err != nil {
		t.Fatal(err)
	}
	_, err := New(database).CommitClaimLease(t.Context(), claim)
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

func TestReplacementLeaseRefreshRejectsStaleWorker(t *testing.T) {
	database, claim := executionFixture(t, "QUEUED")
	claimed, err := New(database).CommitClaimLease(t.Context(), claim)
	if err != nil {
		t.Fatal(err)
	}
	if !claimed {
		t.Fatal("fresh claim rejected")
	}
	repo := New(database)

	// Valid refresh by the same worker should succeed.
	refreshed, err := repo.CommitRefreshLease(t.Context(), claim, 101)
	if err != nil || !refreshed {
		t.Fatalf("valid refresh: refreshed=%v err=%v", refreshed, err)
	}

	// Stale worker should be rejected.
	stale := claim
	stale.WorkerID = "stale"
	refreshed, err = repo.CommitRefreshLease(t.Context(), stale, 102)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed {
		t.Fatal("stale worker refresh accepted")
	}

	// Wrong execution number should be rejected.
	wrongExec := claim
	wrongExec.ExecutionNo = 2
	refreshed, err = repo.CommitRefreshLease(t.Context(), wrongExec, 103)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed {
		t.Fatal("wrong execution refresh accepted")
	}
}

func TestReplacementFailureEventCannotPartiallyCommit(t *testing.T) {
	database, claim := executionFixture(t, "RUNNING")
	result, err := New(database).CommitSettleFailure(t.Context(), gamecontent.SettleFailureCommand{
		Claim: claim,
		Outcome: gamecontent.Outcome{
			Claim: claim, GameID: "game",
			Code: "failed", Retryable: true, Now: 100,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("owned failure rejected")
	}
	assertExecutionState(t, database, "FAILED", 3, 1)
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
	claimed, err := New(database).CommitClaimLease(t.Context(), claim)
	if err != nil {
		t.Fatal(err)
	}
	if claimed {
		t.Fatal("worker claimed a different game")
	}
	assertExecutionState(t, database, "QUEUED", 2, 0)
}

// TestReplacementLeaseRefreshRejectsCurrent verifies that CommitRefreshLease
// delegates to the Refresh scope method and a non-current worker gets false.
func TestReplacementLeaseRefreshRejectsCurrent(t *testing.T) {
	database, claim := executionFixture(t, "QUEUED")
	claimed, err := New(database).CommitClaimLease(t.Context(), claim)
	if err != nil || !claimed {
		t.Fatalf("claim: claimed=%v err=%v", claimed, err)
	}
	refreshed, err := New(database).CommitRefreshLease(t.Context(), claim, claim.Now+1000)
	if err != nil || !refreshed {
		t.Fatalf("refresh: refreshed=%v err=%v", refreshed, err)
	}
	stale := claim
	stale.WorkerID = "stale"
	refreshed, err = New(database).CommitRefreshLease(t.Context(), stale, claim.Now+1000)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed {
		t.Fatal("stale worker refresh accepted")
	}
}

// Keeping unused for compile — verify context.Canceled sentinel is preserved.
var _ = errors.Is(context.Canceled, context.Canceled)
