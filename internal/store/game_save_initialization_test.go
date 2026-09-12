package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/recordstore"
	"retrom/internal/sessionstore"

	"retrom/internal/testassert"
)

func TestFreshGameSavePreservesDataAndBindsRunningLaunches(t *testing.T) {
	t.Parallel()
	database, err := Open(t.Context(), filepath.Join(t.TempDir(), "fresh.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { testassert.False(t, database.Close() != nil, "close fresh database") })
	seedCurrentRuntimeGraph(t, database.SQL)
	_, err = database.SQL.ExecContext(t.Context(), `UPDATE runtime_targets SET checkpoint_json=json_set(checkpoint_json,'$.semantics','GAME_SAVE') WHERE target_id='target-a'`)
	testassert.False(t, err != nil, err)
	tx := lifecycleTransaction(t, database.SQL)
	_, err = sessionstore.CreateLaunch(t.Context(), tx, currentLaunchInsertSQL, "current-launch", "current-game-a", "target-a")
	testassert.False(t, err != nil, err)
	_, err = sessionstore.CreateSave(t.Context(), tx, currentSaveInsertSQL, "current-save", "current-game-a")
	testassert.False(t, err != nil, err)
	_, err = sessionstore.CreateLaunch(t.Context(), tx, `
 INSERT INTO launch_sessions(id,profile_id,game_id,core_id,provider_id,target_id,bundle_sha256,content_kind,
 dependency_snapshot_json,compatibility_code,return_to,credential_sha256,state,
 bootstrap_expires_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms,save_state_id)
 SELECT 'restoring-launch',profile_id,game_id,core_id,provider_id,target_id,bundle_sha256,content_kind,
 dependency_snapshot_json,compatibility_code,return_to,zeroblob(32),state,
 bootstrap_expires_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms,'current-save'
 FROM launch_sessions WHERE id='current-launch'`)
	testassert.False(t, err != nil, err)
	testassert.False(t, tx.Commit() != nil, "commit lifecycle")
	assertInitializedGameSave(t, database.SQL)
	testassert.False(t, database.IntegrityCheck(t.Context()) != nil, "fresh integrity")
}

func assertInitializedGameSave(t *testing.T, database *sql.DB) {
	t.Helper()
	var name, payload, format string
	var created, version, dataVersion int64
	var synced sql.NullInt64
	err := database.QueryRowContext(t.Context(), `SELECT name,payload_blob_id,checkpoint_format,created_at_ms,version,data_version,last_synced_at_ms FROM save_states save JOIN game_save_versions native ON native.save_state_id=save.id WHERE save.id='current-save'`).Scan(&name, &payload, &format, &created, &version, &dataVersion, &synced)
	testassert.False(t, err != nil, err)
	testassert.True(t, name == "Current save" && payload == "current-save-payload" && format == "state-v1" && created == 1 && version == 1 && dataVersion == 1 && !synced.Valid, "save creation changed persisted data")
	var target, frozen string
	var expected int64
	err = database.QueryRowContext(t.Context(), `SELECT save_state_id,restore_payload_blob_id,expected_data_version FROM launch_game_save_bindings WHERE launch_session_id='restoring-launch'`).Scan(&target, &frozen, &expected)
	testassert.False(t, err != nil, err)
	testassert.True(t, target == "current-save" && frozen == payload && expected == 1, "running restore was not frozen")
	var unbound int
	err = database.QueryRowContext(t.Context(), `SELECT count(*) FROM launch_game_save_bindings WHERE launch_session_id='current-launch' AND save_state_id IS NULL AND expected_data_version=0`).Scan(&unbound)
	testassert.False(t, err != nil, err)
	testassert.True(t, unbound == 1, "fresh launch incorrectly adopted a save")
	_, err = sessionstore.ChangeLaunch(t.Context(), database, recordstore.Update{
		Set: `state='FINISHED',finished_at_ms=2,updated_at_ms=2`,
		Scope: recordstore.Scope{
			Where: `id='restoring-launch'`,
		},
	})
	testassert.False(t, err != nil, err)
	err = database.QueryRowContext(t.Context(), `SELECT count(*) FROM launch_game_save_bindings WHERE launch_session_id='restoring-launch' AND restore_payload_blob_id IS NULL AND restore_checkpoint_format IS NULL`).Scan(&unbound)
	testassert.False(t, err != nil, err)
	testassert.True(t, unbound == 1, "finished launch retained frozen restore payload")
}
