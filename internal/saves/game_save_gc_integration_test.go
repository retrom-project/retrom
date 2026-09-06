//go:build integration

package saves

import (
	"testing"

	"retrom/internal/blobregistry"
)

func TestGameSaveFrozenRestoreProtectsOldPayloadUntilLaunchFinishes(t *testing.T) {
	f := newGameSaveFixture(t)
	a := syncGameData(t, f, f.createLaunch(t), "first")
	restoring := f.createLaunchFromSave(t, &a.SaveStateID)
	var oldPayload string
	if err := f.database.SQL.QueryRowContext(t.Context(), `SELECT payload_blob_id FROM save_states WHERE id=?`, a.SaveStateID).Scan(&oldPayload); err != nil {
		t.Fatal(err)
	}
	syncGameData(t, f, restoring, "second")
	assertGameSaveProtection(t, f, oldPayload, true)
	mustSaveSQL(t, f.database.SQL, `UPDATE launch_sessions SET state='FINISHED',finished_at_ms=?,updated_at_ms=? WHERE id=?`, f.now.UnixMilli(), f.now.UnixMilli(), restoring.LaunchID)
	assertGameSaveProtection(t, f, oldPayload, false)
}

func assertGameSaveProtection(t *testing.T, f *saveFixture, id string, expected bool) {
	t.Helper()
	protected, err := blobregistry.ProtectiveSet(t.Context(), f.database.SQL)
	if err != nil {
		t.Fatal(err)
	}
	_, found := protected[id]
	if found != expected {
		t.Fatalf("frozen input protected=%v, want %v", found, expected)
	}
}
