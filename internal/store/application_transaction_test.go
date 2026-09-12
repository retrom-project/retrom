package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"retrom/internal/recordstore"
	"retrom/internal/sessionstore"
)

func TestApplicationAtomicCancellationRollsBackContinuingTransaction(t *testing.T) {
	t.Parallel()
	fixture := openEmulationStationSchemaFixture(t)
	db := fixture.database.SQL
	tx := lifecycleTransaction(t, db)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, err := recordstore.Atomic(ctx, tx, func(connection recordstore.DBTX) (sql.Result, error) {
		result, writeErr := connection.ExecContext(ctx,
			"INSERT INTO profiles(id,display_name,created_at_ms) VALUES('cancelled-profile','Cancelled',1)")
		cancel()
		return result, writeErr
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM profiles WHERE id='cancelled-profile'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("cancelled write retained %d profiles, error = %v", count, err)
	}
}

func TestInvalidSessionCreationDoesNotLeakIntoContinuingTransaction(t *testing.T) {
	t.Parallel()
	db := lifecycleDatabase(t)
	tx := lifecycleTransaction(t, db)
	_, err := sessionstore.CreateLaunch(t.Context(), tx, currentLaunchInsertSQL,
		"retry-launch", "current-game-a", "target-b")
	if !errors.Is(err, recordstore.ErrInvariant) {
		t.Fatalf("foreign target error = %v", err)
	}
	_, err = sessionstore.CreateLaunch(t.Context(), tx, currentLaunchInsertSQL,
		"retry-launch", "current-game-a", "target-a")
	if err != nil {
		t.Fatalf("invalid creation retained its identity: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM launch_sessions launch
 JOIN launch_payload_retirements retirement ON retirement.launch_session_id=launch.id
 WHERE launch.id='retry-launch' AND launch.target_id='target-a'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("committed valid launch and retirement = %d, error = %v", count, err)
	}
}

func TestBatchAdminDowngradeRollsBackEverySelectedUser(t *testing.T) {
	t.Parallel()
	fixture := openEmulationStationSchemaFixture(t)
	db := fixture.database.SQL
	if _, err := db.ExecContext(t.Context(), `
 INSERT INTO profiles(id,display_name,created_at_ms) VALUES('second-profile','Second',1);
 INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
 VALUES('second-admin','second-profile','second-admin','Second','ADMIN','ENABLED',1,1);`); err != nil {
		t.Fatal(err)
	}
	tx := lifecycleTransaction(t, db)
	_, err := recordstore.UpdateUsers(t.Context(), tx, recordstore.Update{
		Set: "role='USER'", Scope: recordstore.Scope{Where: "role='ADMIN'"},
	})
	if !errors.Is(err, recordstore.ErrInvariant) {
		t.Fatalf("batch removed all administrators: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM users WHERE role='ADMIN' AND status='ENABLED'").Scan(&count); err != nil || count != 2 {
		t.Fatalf("surviving admins = %d, error = %v", count, err)
	}
}
