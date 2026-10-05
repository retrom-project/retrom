//go:build integration

package saves

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	saveservice "retrom/internal/service/saves"

	"github.com/google/uuid"
)

func newGameSaveFixture(t *testing.T) *saveFixture {
	t.Helper()
	f := newSaveFixture(t)
	mustSaveSQL(t, f.database.SQL, `UPDATE runtime_targets
SET checkpoint_json=(jsonb_set((checkpoint_json)::jsonb,'{semantics}',to_jsonb(('GAME_SAVE')::text),true))::text
WHERE (provider_id,target_id) IN (SELECT provider_id,target_id FROM game_variants WHERE game_id=?)`, f.gameID)
	return f
}

func syncGameData(t *testing.T, f *saveFixture, launch saveLaunch, value string) saveservice.ManualResult {
	t.Helper()
	result, _, err := f.saves.CreateManual(f.ctx, launch.LaunchID, launch.Capability,
		uuid.NewString(), manualRequest(t, "自动同步存档", []byte(value), screenshotPNG(t)))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestGameSaveUpdatesOneSlotAndKeepsUserName(t *testing.T) {
	f := newGameSaveFixture(t)
	session := f.createLaunch(t)
	a := syncGameData(t, f, session, "settings")
	mustSaveSQL(t, f.database.SQL, `UPDATE save_states SET name='我的周目',version=version+1 WHERE id=?`, a.SaveStateID)
	*f.now = f.now.Add(time.Second)
	b := syncGameData(t, f, session, "progress")
	if a.SaveStateID != b.SaveStateID {
		t.Fatalf("sync created another slot: %s -> %s", a.SaveStateID, b.SaveStateID)
	}
	var count int
	var name string
	var created, updated int64
	if err := dbapi.QueryRowContext(t.Context(), f.database.SQL, `SELECT name,created_at_ms,updated_at_ms FROM save_states WHERE id=?`, a.SaveStateID).Scan(&name, &created, &updated); err != nil {
		t.Fatal(err)
	}
	if name != "我的周目" || updated <= created {
		t.Fatalf("name/time changed incorrectly: %s %d %d", name, created, updated)
	}
	if err := dbapi.QueryRowContext(t.Context(), f.database.SQL, `SELECT count(*) FROM save_states WHERE game_id=?`, f.gameID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("slots=%d", count)
	}
	c := syncGameData(t, f, f.createLaunch(t), "other playthrough")
	if c.SaveStateID == a.SaveStateID {
		t.Fatal("new game reused prior slot")
	}
}

func TestGameSaveRestoreUsesCurrentSelectedSlot(t *testing.T) {
	f := newGameSaveFixture(t)
	a := syncGameData(t, f, f.createLaunch(t), "first")
	session := f.createLaunchFromSave(t, &a.SaveStateID)
	b := syncGameData(t, f, session, "second")
	if b.SaveStateID != a.SaveStateID {
		t.Fatal("restore did not update selected slot")
	}
	digest, err := f.saves.StateFile(f.ctx, session.LaunchID, session.Capability)
	expected := sha256.Sum256([]byte("second"))
	if err != nil || digest.Digest != hex.EncodeToString(expected[:]) {
		t.Fatalf("restore did not use current slot: %s %v", digest.Digest, err)
	}
	next := f.createLaunchFromSave(t, &a.SaveStateID)
	digest, err = f.saves.StateFile(f.ctx, next.LaunchID, next.Capability)
	expected = sha256.Sum256([]byte("second"))
	if err != nil || digest.Digest != hex.EncodeToString(expected[:]) {
		t.Fatalf("next restore is stale: %s %v", digest.Digest, err)
	}
}

func TestGameSaveRejectsStaleWriterAndDeletedSlot(t *testing.T) {
	f := newGameSaveFixture(t)
	a := syncGameData(t, f, f.createLaunch(t), "initial")
	stale := f.createLaunchFromSave(t, &a.SaveStateID)
	active := f.createLaunchFromSave(t, &a.SaveStateID)
	syncGameData(t, f, active, "newer")
	if _, err := f.saves.StateFile(f.ctx, stale.LaunchID, stale.Capability); !errors.Is(err, saveservice.ErrCheckpointIncompatible) {
		t.Fatalf("stale launch restored a changed save: %v", err)
	}
	_, _, err := f.saves.CreateManual(f.ctx, stale.LaunchID, stale.Capability, uuid.NewString(), manualRequest(t, "stale", []byte("older"), screenshotPNG(t)))
	if !errors.Is(err, saveservice.ErrSyncConflict) {
		t.Fatal("stale writer replaced newer data")
	}
	mustSaveSQL(t, f.database.SQL, `UPDATE save_states SET deleted_at_ms=?,version=version+1 WHERE id=?`, f.now.UnixMilli(), a.SaveStateID)
	_, _, err = f.saves.CreateManual(f.ctx, active.LaunchID, active.Capability, uuid.NewString(), manualRequest(t, "deleted", []byte("deleted"), screenshotPNG(t)))
	if !errors.Is(err, saveservice.ErrSyncConflict) {
		t.Fatal("deleted slot was recreated")
	}
}

func TestGameSaveRequiresScreenshotAndDeduplicatesPayload(t *testing.T) {
	f := newGameSaveFixture(t)
	session := f.createLaunch(t)
	_, _, err := f.saves.CreateManual(f.ctx, session.LaunchID, session.Capability, uuid.NewString(), manualRequest(t, "empty image", []byte("first"), nil))
	if err == nil {
		t.Fatal("native sync without screenshot was published")
	}
	a := syncGameData(t, f, session, "first")
	var before, after int64
	if err = dbapi.QueryRowContext(t.Context(), f.database.SQL, `SELECT version FROM save_states WHERE id=?`, a.SaveStateID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	*f.now = f.now.Add(time.Second)
	b := syncGameData(t, f, session, "first")
	if err = dbapi.QueryRowContext(t.Context(), f.database.SQL, `SELECT version FROM save_states WHERE id=?`, a.SaveStateID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if a.SaveStateID != b.SaveStateID || before != after {
		t.Fatal("identical data created a new version")
	}
}
