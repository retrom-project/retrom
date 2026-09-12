package store

import (
	"database/sql"
	"strings"
	"testing"

	"retrom/internal/recordstore"

	"retrom/internal/sessionstore"
)

func TestLaunchRevocationUpdatesOnlyChangedSessionsAndPreservesPriorRevocation(t *testing.T) {
	t.Parallel()
	db := lifecycleDatabase(t)
	tx := lifecycleTransaction(t, db)
	for index, id := range []string{"current-launch", "prior-launch", "other-launch"} {
		if _, err := sessionstore.CreateLaunch(t.Context(), tx, currentLaunchInsertSQL,
			id, "current-game-a", "target-a"); err != nil {
			t.Fatal(err)
		}
		seedLaunchCapability(t, tx, id, byte(index+1))
	}
	if _, err := recordstore.UpdateIsolatedRuntimeCapabilities(t.Context(), tx, recordstore.Update{
		Set: `revoked_at_ms=2`,
		Scope: recordstore.Scope{
			Where: `launch_id='prior-launch'`,
		},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := sessionstore.ChangeLaunch(t.Context(), tx, recordstore.Update{
		Set: `state='REVOKED',finished_at_ms=3,updated_at_ms=3`,
		Scope: recordstore.Scope{
			Where: `id IN ('current-launch','prior-launch') AND state='CREATED'`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := result.RowsAffected(); err != nil || count != 2 {
		t.Fatalf("revoked session count = %d, error = %v", count, err)
	}
	for id, want := range map[string]int64{"current-launch": 3, "prior-launch": 2, "other-launch": 0} {
		var actual int64
		if err := tx.QueryRowContext(t.Context(), `SELECT COALESCE(revoked_at_ms,0)
FROM isolated_runtime_capabilities WHERE launch_id=?`, id).Scan(&actual); err != nil || actual != want {
			t.Fatalf("%s revocation = %d, want %d; error = %v", id, actual, want, err)
		}
	}
}

func seedLaunchCapability(t *testing.T, tx *sql.Tx, id string, seed byte) {
	t.Helper()
	digest := []byte(strings.Repeat(string([]byte{seed}), 32))
	if _, err := tx.ExecContext(t.Context(), `INSERT INTO isolated_runtime_bootstrap_tickets(
ticket_sha256,launch_id,profile_id,expected_origin,expires_at_ms,consumed_at_ms)
VALUES(?,?,'current-profile',?,10,1)`, digest, id, "https://"+id+".runtime.example"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), `INSERT INTO isolated_runtime_capabilities(
credential_sha256,launch_id,profile_id,expected_origin,issued_at_ms,expires_at_ms)
VALUES(?,?,'current-profile',?,1,20)`, digest, id, "https://"+id+".runtime.example"); err != nil {
		t.Fatal(err)
	}
}

func TestLaunchLifecycleRollbackDoesNotLeakRows(t *testing.T) {
	t.Parallel()
	db := lifecycleDatabase(t)
	tx := lifecycleTransaction(t, db)
	if _, err := tx.ExecContext(t.Context(), `DROP TABLE launch_payload_retirements`); err != nil {
		t.Fatal(err)
	}
	if _, err := sessionstore.CreateLaunch(t.Context(), tx, currentLaunchInsertSQL,
		"current-launch", "current-game-a", "target-a"); err == nil {
		t.Fatal("missing retirement storage did not abort creation")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM launch_sessions`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rolled back launch count = %d, error = %v", count, err)
	}
}

func TestLaunchLifecycleNoOpDoesNotRequeueReleasedPayload(t *testing.T) {
	t.Parallel()
	db := lifecycleDatabase(t)
	tx := lifecycleTransaction(t, db)
	if _, err := sessionstore.CreateLaunch(t.Context(), tx, currentLaunchInsertSQL,
		"current-launch", "current-game-a", "target-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), `UPDATE launch_payload_retirements
SET released_at_ms=2 WHERE launch_session_id='current-launch'`); err != nil {
		t.Fatal(err)
	}
	result, err := sessionstore.ChangeLaunch(t.Context(), tx, recordstore.Update{
		Set: `idle_expires_at_ms=15`,
		Scope: recordstore.Scope{
			Where: `id='current-launch' AND version=100`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := result.RowsAffected(); err != nil || count != 0 {
		t.Fatalf("stale update count = %d, error = %v", count, err)
	}
	if _, err := sessionstore.ChangeLaunch(t.Context(), tx, recordstore.Update{
		Set: `idle_expires_at_ms=15,state='ACTIVE',activated_at_ms=3,updated_at_ms=3`,
		Scope: recordstore.Scope{
			Where: `id='current-launch'`,
		},
	}); err != nil {
		t.Fatal(err)
	}
	var due, released int64
	if err := tx.QueryRowContext(t.Context(), `SELECT due_at_ms,released_at_ms
FROM launch_payload_retirements WHERE launch_session_id='current-launch'`).Scan(&due, &released); err != nil || due != 10 || released != 2 {
		t.Fatalf("released retirement changed: due=%d released=%d error=%v", due, released, err)
	}
}
