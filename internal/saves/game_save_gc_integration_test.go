//go:build integration

package saves

import (
	"testing"

	"retrom/internal/recordstore"

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
	mustUpdateLaunch(t, f.database.SQL, recordstore.Update{Set: `state='FINISHED',finished_at_ms=?,updated_at_ms=?`, Scope: recordstore.Scope{Where: `id=?`, Args: []any{restoring.LaunchID}}, Values: []any{f.now.UnixMilli(), f.now.UnixMilli()}})
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
