//go:build integration

package saves

import (
	"testing"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
)

func TestGameSaveUpdatedPayloadReleasesOldBlobWhileLaunchExists(t *testing.T) {
	f := newGameSaveFixture(t)
	a := syncGameData(t, f, f.createLaunch(t), "first")
	restoring := f.createLaunchFromSave(t, &a.SaveStateID)
	var oldPayload string
	if err := dbapi.QueryRowContext(t.Context(), f.database.SQL, `SELECT payload_file_record FROM save_states WHERE id=?`, a.SaveStateID).Scan(&oldPayload); err != nil {
		t.Fatal(err)
	}
	syncGameData(t, f, restoring, "second")
	assertGameSaveProtection(t, f, oldPayload, false)
	mustUpdateLaunch(t, f.database.SQL, recordstore.Update{Set: `state='FINISHED',finished_at_ms=?,updated_at_ms=?`, Scope: recordstore.Scope{Where: `id=?`, Args: []any{restoring.LaunchID}}, Values: []any{f.now.UnixMilli(), f.now.UnixMilli()}})
	assertGameSaveProtection(t, f, oldPayload, false)
}

func assertGameSaveProtection(t *testing.T, f *saveFixture, id string, expected bool) {
	t.Helper()
	var retained bool
	err := dbapi.QueryRowContext(t.Context(), f.database.SQL, `SELECT EXISTS(SELECT 1 FROM save_states WHERE payload_file_record=? AND deleted_at_ms IS NULL)`, id).Scan(&retained)
	if err != nil {
		t.Fatal(err)
	}
	if retained != expected {
		t.Fatalf("frozen input retained=%v, want %v", retained, expected)
	}
}
