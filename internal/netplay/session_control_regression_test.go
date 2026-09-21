package netplay

import (
	"errors"
	"reflect"
	"testing"
)

func TestUnknownParticipantCannotPauseSession(t *testing.T) {
	t.Parallel()
	fixture := readyControlFixture(t)
	room, err := fixture.service.Start(t.Context(), fixture.room.RoomID, "host", fixture.room.Version)
	if err != nil {
		t.Fatal(err)
	}
	fixture.room = room
	if _, err := fixture.database.ExecContext(t.Context(), `UPDATE netplay_sessions SET state='RUNNING' WHERE id=?`, room.CurrentSession.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.ExecContext(t.Context(), `INSERT INTO profiles(id,display_name,created_at_ms)VALUES('absent','Absent',?)`, fixture.now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	before := roomExitRecordsSnapshot(t, fixture)
	err = fixture.service.MarkDisconnected(t.Context(), SocketParticipant{RoomID: room.RoomID, SessionID: room.CurrentSession.SessionID, ProfileID: "absent", PlayerNo: 2})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("unknown participant disconnect error=%v", err)
	}
	if after := roomExitRecordsSnapshot(t, fixture); !reflect.DeepEqual(before, after) {
		t.Fatalf("unknown participant changed session before=%v after=%v", before, after)
	}
}
