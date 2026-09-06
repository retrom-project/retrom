package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/testassert"
)

func TestGameSaveUpgradePreservesSavedDataAndBindsRunningLaunches(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "upgrade.db")
	old := openMigrationTestDatabase(t, path)
	sources, err := migrationSources()
	testassert.False(t, err != nil, err)
	for _, source := range sources[:11] {
		testassert.False(t, runMigration(t.Context(), old, source, time.Now) != nil, source.name)
	}
	seedCurrentRuntimeGraph(t, old)
	_, err = old.ExecContext(t.Context(), `UPDATE runtime_targets SET checkpoint_json=json_set(checkpoint_json,'$.semantics','GAME_SAVE') WHERE target_id='target-a'`)
	testassert.False(t, err != nil, err)
	_, err = old.ExecContext(t.Context(), currentLaunchInsertSQL, "current-launch", "current-game-a", "target-a")
	testassert.False(t, err != nil, err)
	_, err = old.ExecContext(t.Context(), currentSaveInsertSQL, "current-save", "current-game-a")
	testassert.False(t, err != nil, err)
	_, err = old.ExecContext(t.Context(), currentLaunchInsertSQL, "restoring-launch", "current-game-a", "target-a")
	testassert.False(t, err != nil, err)
	_, err = old.ExecContext(t.Context(), `UPDATE launch_sessions SET save_state_id='current-save' WHERE id='restoring-launch'`)
	testassert.False(t, err != nil, err)
	testassert.False(t, old.Close() != nil, "close old schema")
	current, err := Open(t.Context(), path, time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { testassert.False(t, current.Close() != nil, "close upgraded database") })
	assertUpgradedGameSave(t, current.SQL)
	testassert.False(t, current.IntegrityCheck(t.Context()) != nil, "upgraded integrity")
}

func assertUpgradedGameSave(t *testing.T, database *sql.DB) {
	t.Helper()
	var name, payload, format string
	var created, version, dataVersion int64
	var synced sql.NullInt64
	err := database.QueryRowContext(t.Context(), `SELECT name,payload_blob_id,checkpoint_format,created_at_ms,version,data_version,last_synced_at_ms FROM save_states save JOIN game_save_versions native ON native.save_state_id=save.id WHERE save.id='current-save'`).Scan(&name, &payload, &format, &created, &version, &dataVersion, &synced)
	testassert.False(t, err != nil, err)
	testassert.True(t, name == "Current save" && payload == "current-save-payload" && format == "state-v1" && created == 1 && version == 1 && dataVersion == 1 && !synced.Valid, "upgrade changed existing save")
	var target, frozen string
	var expected int64
	err = database.QueryRowContext(t.Context(), `SELECT save_state_id,restore_payload_blob_id,expected_data_version FROM launch_game_save_bindings WHERE launch_session_id='restoring-launch'`).Scan(&target, &frozen, &expected)
	testassert.False(t, err != nil, err)
	testassert.True(t, target == "current-save" && frozen == payload && expected == 1, "running restore was not frozen")
	var unbound int
	err = database.QueryRowContext(t.Context(), `SELECT count(*) FROM launch_game_save_bindings WHERE launch_session_id='current-launch' AND save_state_id IS NULL AND expected_data_version=0`).Scan(&unbound)
	testassert.False(t, err != nil, err)
	testassert.True(t, unbound == 1, "fresh launch incorrectly adopted a save")
	_, err = database.ExecContext(t.Context(), `UPDATE launch_sessions SET state='FINISHED',finished_at_ms=2,updated_at_ms=2 WHERE id='restoring-launch'`)
	testassert.False(t, err != nil, err)
	err = database.QueryRowContext(t.Context(), `SELECT count(*) FROM launch_game_save_bindings WHERE launch_session_id='restoring-launch' AND restore_payload_blob_id IS NULL AND restore_checkpoint_format IS NULL`).Scan(&unbound)
	testassert.False(t, err != nil, err)
	testassert.True(t, unbound == 1, "finished launch retained frozen restore payload")
}
