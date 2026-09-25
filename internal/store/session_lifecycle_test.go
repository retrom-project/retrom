package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/persistence/recordstore"

	"retrom/internal/persistence/sessionstore"
)

func lifecycleDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), func() time.Time {
		return time.UnixMilli(1786000000000)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	seedCurrentRuntimeGraph(t, database.SQL)

	return database.SQL
}

func lifecycleTransaction(t *testing.T, database *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

func TestLaunchCreationSchedulesRetirementWithoutTriggers(t *testing.T) {
	t.Parallel()
	db := lifecycleDatabase(t)
	tx := lifecycleTransaction(t, db)
	if _, err := sessionstore.CreateLaunch(t.Context(), tx, currentLaunchInsertSQL,
		"current-launch", "current-game-a", "target-a"); err != nil {
		t.Fatal(err)
	}
	var due int64
	if err := tx.QueryRowContext(t.Context(), `SELECT due_at_ms FROM launch_payload_retirements
WHERE launch_session_id='current-launch'`).Scan(&due); err != nil || due != 10 {
		t.Fatalf("creation retirement deadline = %d, error = %v", due, err)
	}
	if _, err := sessionstore.ChangeLaunch(t.Context(), tx, recordstore.Update{
		Set: `state='ACTIVE',activated_at_ms=2,idle_expires_at_ms=15,updated_at_ms=2`,
		Scope: recordstore.Scope{
			Where: `id='current-launch'`,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRowContext(t.Context(), `SELECT due_at_ms FROM launch_payload_retirements
WHERE launch_session_id='current-launch'`).Scan(&due); err != nil || due != 20 {
		t.Fatalf("active retirement deadline = %d, error = %v", due, err)
	}
}

func TestPlayResilienceMigrationClearsActiveIdleRetirement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retrom.db")
	db := openMigrationTestDatabase(t, path)
	defer func() { _ = db.Close() }()
	sources, err := migrationSources()
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources[:len(sources)-1] {
		if err := runMigration(t.Context(), db, source, time.Now); err != nil {
			t.Fatal(err)
		}
	}
	seedCurrentRuntimeGraph(t, db)
	tx := lifecycleTransaction(t, db)
	if _, err := sessionstore.CreateLaunch(t.Context(), tx, currentLaunchInsertSQL,
		"current-launch", "current-game-a", "target-a"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE launch_sessions SET state='ACTIVE',activated_at_ms=2,idle_expires_at_ms=15
WHERE id='current-launch'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE launch_payload_retirements SET due_at_ms=15
WHERE launch_session_id='current-launch'`); err != nil {
		t.Fatal(err)
	}
	if err := runMigration(t.Context(), db, sources[len(sources)-1], time.Now); err != nil {
		t.Fatal(err)
	}
	var idle sql.NullInt64
	var due, hard int64
	if err := db.QueryRowContext(t.Context(), `SELECT launch.idle_expires_at_ms,retirement.due_at_ms,launch.hard_expires_at_ms
FROM launch_sessions launch JOIN launch_payload_retirements retirement ON retirement.launch_session_id=launch.id
WHERE launch.id='current-launch'`).Scan(&idle, &due, &hard); err != nil {
		t.Fatal(err)
	}
	if idle.Valid || due != hard {
		t.Fatalf("idle=%v due=%d hard=%d", idle, due, hard)
	}
}

func TestSaveCreationInitializesDataVersionWithoutTriggers(t *testing.T) {
	t.Parallel()
	db := lifecycleDatabase(t)
	tx := lifecycleTransaction(t, db)
	if _, err := sessionstore.CreateLaunch(t.Context(), tx, currentLaunchInsertSQL,
		"current-launch", "current-game-a", "target-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := sessionstore.CreateSave(t.Context(), tx, currentSaveInsertSQL,
		"current-save", "current-game-a"); err != nil {
		t.Fatal(err)
	}
	var version int64
	if err := tx.QueryRowContext(t.Context(), `SELECT data_version FROM game_save_versions
WHERE save_state_id='current-save'`).Scan(&version); err != nil || version != 1 {
		t.Fatalf("new save data version = %d, error = %v", version, err)
	}
}

func TestNativeLaunchFreezesAndReleasesRestoreInputWithoutTriggers(t *testing.T) {
	t.Parallel()
	db := lifecycleDatabase(t)
	tx := lifecycleTransaction(t, db)
	if _, err := tx.ExecContext(t.Context(), `UPDATE runtime_targets
SET checkpoint_json=json_set(checkpoint_json,'$.semantics','GAME_SAVE') WHERE target_id='target-a'`); err != nil {
		t.Fatal(err)
	}
	if _, err := sessionstore.CreateLaunch(t.Context(), tx, currentLaunchInsertSQL,
		"current-launch", "current-game-a", "target-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := sessionstore.CreateSave(t.Context(), tx, currentSaveInsertSQL,
		"current-save", "current-game-a"); err != nil {
		t.Fatal(err)
	}
	query := `INSERT INTO launch_sessions(
id,profile_id,game_id,core_id,provider_id,target_id,bundle_sha256,content_kind,
dependency_snapshot_json,compatibility_code,return_to,credential_sha256,state,
bootstrap_expires_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms,save_state_id)
SELECT 'restoring-launch',profile_id,game_id,core_id,provider_id,target_id,bundle_sha256,content_kind,
dependency_snapshot_json,compatibility_code,return_to,zeroblob(32),state,
bootstrap_expires_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms,'current-save'
FROM launch_sessions WHERE id='current-launch'`
	if _, err := sessionstore.CreateLaunch(t.Context(), tx, query); err != nil {
		t.Fatal(err)
	}
	var blob string
	var version int64
	if err := tx.QueryRowContext(t.Context(), `SELECT restore_payload_blob_id,expected_data_version
FROM launch_game_save_bindings WHERE launch_session_id='restoring-launch'`).Scan(&blob, &version); err != nil || blob != "current-save-payload" || version != 1 {
		t.Fatalf("frozen restore = %q/%d, error = %v", blob, version, err)
	}
	if _, err := sessionstore.ChangeLaunch(t.Context(), tx, recordstore.Update{
		Set: `state='FINISHED',finished_at_ms=2,updated_at_ms=2`,
		Scope: recordstore.Scope{
			Where: `id='restoring-launch'`,
		},
	}); err != nil {
		t.Fatal(err)
	}
	var released int
	if err := tx.QueryRowContext(t.Context(), `SELECT count(*) FROM launch_game_save_bindings
WHERE launch_session_id='restoring-launch' AND restore_payload_blob_id IS NULL
AND restore_checkpoint_format IS NULL`).Scan(&released); err != nil || released != 1 {
		t.Fatalf("released restore count = %d, error = %v", released, err)
	}
}
