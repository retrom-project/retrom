//go:build integration

package saves

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestLocalDraftCommitsAfterFinishWithoutRuntimeCapability(t *testing.T) {
	f := newGameSaveFixture(t)
	source := syncGameData(t, f, f.createLaunch(t), "original")
	run := f.createLaunchFromSave(t, &source.SaveStateID)
	mustSaveSQL(t, f.database.SQL, `UPDATE launch_sessions SET state='FINISHED',finished_at_ms=? WHERE id=?`, f.now.UnixMilli(), run.LaunchID)
	key := uuid.NewString()
	request := func() (ManualResult, bool, error) {
		return f.saves.CreateLocalDraft(f.ctx, run.LaunchID, "local", "local", key,
			manualRequest(t, "confirmed", []byte("confirmed"), screenshotPNG(t)))
	}
	saved, replayed, err := request()
	if err != nil || replayed || saved.SaveStateID != source.SaveStateID {
		t.Fatalf("commit: %#v %v %v", saved, replayed, err)
	}
	again, replayed, err := request()
	if err != nil || !replayed || again.SaveStateID != saved.SaveStateID {
		t.Fatalf("replay: %#v %v %v", again, replayed, err)
	}
	_, _, err = f.saves.CreateManual(f.ctx, run.LaunchID, run.Capability, uuid.NewString(), manualRequest(t, "runtime", []byte("wrong"), screenshotPNG(t)))
	if !errors.Is(err, ErrCredential) {
		t.Fatalf("finished runtime credential admitted: %v", err)
	}
}

func TestLocalDraftCannotWriteAnotherAccountOrRevokedOrInstantLaunch(t *testing.T) {
	f := newGameSaveFixture(t)
	run := f.createLaunch(t)
	for _, owner := range [][2]string{{"other", "local"}, {"local", "other"}} {
		_, _, err := f.saves.CreateLocalDraft(f.ctx, run.LaunchID, owner[0], owner[1], uuid.NewString(),
			manualRequest(t, "draft", []byte("draft"), screenshotPNG(t)))
		if !errors.Is(err, ErrCredential) {
			t.Fatalf("foreign owner accepted: %v", err)
		}
	}
	mustSaveSQL(t, f.database.SQL, `UPDATE launch_sessions SET state='REVOKED',finished_at_ms=? WHERE id=?`, f.now.UnixMilli(), run.LaunchID)
	_, _, err := f.saves.CreateLocalDraft(f.ctx, run.LaunchID, "local", "local", uuid.NewString(), manualRequest(t, "draft", []byte("draft"), screenshotPNG(t)))
	if !errors.Is(err, ErrCredential) {
		t.Fatalf("revoked accepted: %v", err)
	}
	instant := newSaveFixture(t)
	ordinary := instant.createLaunch(t)
	_, _, err = instant.saves.CreateLocalDraft(instant.ctx, ordinary.LaunchID, "local", "local", uuid.NewString(), manualRequest(t, "draft", []byte("draft"), screenshotPNG(t)))
	if !errors.Is(err, ErrCredential) {
		t.Fatalf("instant accepted: %v", err)
	}
}

func TestLocalDraftRetainsSourceVersionAndFreshLaunchHasNoRestore(t *testing.T) {
	f := newGameSaveFixture(t)
	a := syncGameData(t, f, f.createLaunch(t), "source")
	stale := f.createLaunchFromSave(t, &a.SaveStateID)
	newer := f.createLaunchFromSave(t, &a.SaveStateID)
	syncGameData(t, f, newer, "newer")
	_, _, err := f.saves.CreateLocalDraft(f.ctx, stale.LaunchID, "local", "local", uuid.NewString(), manualRequest(t, "old", []byte("old"), screenshotPNG(t)))
	if !errors.Is(err, ErrSyncConflict) {
		t.Fatalf("stale draft replaced newer progress: %v", err)
	}
	fresh := f.createLaunch(t)
	var empty int
	err = f.database.SQL.QueryRowContext(t.Context(), `SELECT count(*) FROM launch_game_save_bindings binding JOIN launch_sessions session ON session.id=binding.launch_session_id
 WHERE session.id=? AND session.save_state_id IS NULL AND binding.save_state_id IS NULL AND binding.restore_payload_blob_id IS NULL`, fresh.LaunchID).Scan(&empty)
	if err != nil || empty != 1 {
		t.Fatalf("fresh launch inherited old data: %d %v", empty, err)
	}
	b, _, err := f.saves.CreateLocalDraft(f.ctx, fresh.LaunchID, "local", "local", uuid.NewString(), manualRequest(t, "new play", []byte("new play"), screenshotPNG(t)))
	if err != nil || b.SaveStateID == a.SaveStateID {
		t.Fatalf("new play overwrote source: %#v %v", b, err)
	}
	deleted := f.createLaunchFromSave(t, &a.SaveStateID)
	mustSaveSQL(t, f.database.SQL, `UPDATE save_states SET deleted_at_ms=? WHERE id=?`, f.now.UnixMilli(), a.SaveStateID)
	_, _, err = f.saves.CreateLocalDraft(f.ctx, deleted.LaunchID, "local", "local", uuid.NewString(), manualRequest(t, "deleted", []byte("deleted"), screenshotPNG(t)))
	if !errors.Is(err, ErrSyncConflict) {
		t.Fatalf("deleted target recreated: %v", err)
	}
}
