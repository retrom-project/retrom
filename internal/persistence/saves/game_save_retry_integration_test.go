//go:build integration

package saves

import (
	"testing"

	dbapi "retrom/internal/database"

	"github.com/google/uuid"
)

func TestGameSaveFailedUpdateAndLateRetryCannotReplaceCommittedData(t *testing.T) {
	f := newGameSaveFixture(t)
	session := f.createLaunch(t)
	var count int
	if err := dbapi.QueryRowContext(t.Context(), f.database.SQL, `SELECT count(*) FROM save_states`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("launch created a save before data changed: %d %v", count, err)
	}
	key := uuid.NewString()
	first, _, err := f.saves.CreateManual(f.ctx, session.LaunchID, session.Capability, key,
		manualRequest(t, "first", []byte("first"), screenshotPNG(t)))
	if err != nil {
		t.Fatal(err)
	}
	second := syncGameData(t, f, session, "second")
	var oldPayload, oldImage, newPayload, newImage string
	if err = dbapi.QueryRowContext(t.Context(), f.database.SQL, `SELECT payload_file_record,screenshot_file_record FROM save_states WHERE id=?`, first.SaveStateID).Scan(&oldPayload, &oldImage); err != nil {
		t.Fatal(err)
	}
	_, _, err = f.saves.CreateManual(f.ctx, session.LaunchID, session.Capability,
		uuid.NewString(), manualRequest(t, "bad", []byte("bad"), nil))
	if err == nil {
		t.Fatal("incomplete native update succeeded")
	}
	replay, _, err := f.saves.CreateManual(f.ctx, session.LaunchID, session.Capability, key,
		manualRequest(t, "first", []byte("first"), screenshotPNG(t)))
	if err != nil || replay.SaveStateID != first.SaveStateID {
		t.Fatalf("retry duplicated: %#v %v", replay, err)
	}
	if err = dbapi.QueryRowContext(t.Context(), f.database.SQL, `SELECT payload_file_record,screenshot_file_record FROM save_states WHERE id=?`, second.SaveStateID).Scan(&newPayload, &newImage); err != nil {
		t.Fatal(err)
	}
	if newPayload != oldPayload || newImage != oldImage {
		t.Fatal("failed update or late replay rolled back committed payload/image")
	}
	if err = dbapi.QueryRowContext(t.Context(), f.database.SQL, `SELECT count(*) FROM save_states`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retry duplicated save: %d %v", count, err)
	}
}
