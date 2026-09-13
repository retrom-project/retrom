package netplay

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	repository "retrom/internal/persistence/netplay"
	application "retrom/internal/service/netplay"
)

type controlFixture struct {
	database *sql.DB
	service  *Service
	room     Room
	now      time.Time
}

func newControlFixture(t *testing.T) controlFixture {
	t.Helper()
	now := time.UnixMilli(1_786_000_000_000)
	database := openNetplayTestDatabase(t.Context(), t, func() time.Time { return now })
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	seedControlHost(t, database.SQL, now)
	gameID := seedControlGame(t, database.SQL, now)
	if _, err := database.SQL.ExecContext(t.Context(), `INSERT INTO profiles(id,display_name,created_at_ms)VALUES('guest','Guest',?)`, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "data", ManifestRelativePath))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := parseRegistry(raw, fixtureDependencySet())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(database.SQL, registry, nil, Options{MaxActiveRooms: 16, DraftIdle: time.Hour, WaitingIdle: time.Hour}, func() time.Time { return now })
	room, err := service.CreateRoom(t.Context(), "host")
	if err != nil {
		t.Fatal(err)
	}
	room, err = service.SelectGame(t.Context(), room.RoomID, "host", gameID, "fceumm-423-v1", room.Version)
	if err != nil {
		t.Fatal(err)
	}
	room, err = service.SetSeat(t.Context(), room.RoomID, "guest", 2, room.Version)
	if err != nil {
		t.Fatal(err)
	}
	room, err = service.Room(t.Context(), room.RoomID, "host")
	if err != nil {
		t.Fatal(err)
	}
	return controlFixture{database.SQL, service, room, now}
}

func TestRoomControlFailuresRollBackVersionsMembersAndEvents(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"select", "clear", "seat", "ready", "stale room", "stale member"} {
		t.Run(action, func(t *testing.T) {
			assertControlRollback(t, action)
		})
	}
}

func applyControlTestWrite(t *testing.T, scope application.RoomControlScope, before application.RoomControlSnapshot, evidence application.RoomControlEvidence, action string) error {
	t.Helper()
	switch action {
	case "select":
		evidence.Type = "GAME_SELECTED"
		return scope.Write.Select(t.Context(), application.RoomSelectionPlan{Before: before, Selection: *before.Selection, Evidence: evidence})
	case "clear":
		evidence.Type = "GAME_CLEARED"
		return scope.Write.Clear(t.Context(), application.RoomClearPlan{Before: before, Evidence: evidence})
	case "seat":
		evidence.Type = "SEAT_CHANGED"
		return scope.Write.Seat(t.Context(), application.RoomSeatPlan{Before: before, MemberID: before.Member.ID, PlayerNo: 2, Evidence: evidence})
	default:
		evidence.Type = "READY_CHANGED"
		if action == "stale member" {
			before.Member.Version++
		}
		plan := application.RoomReadyPlan{Before: before, Ready: true, Evidence: evidence}
		if err := scope.Write.Ready(t.Context(), plan); err != nil {
			return err
		}
		if action == "stale room" {
			return scope.Write.Ready(t.Context(), plan)
		}
		return nil
	}
}

func controlCounts(t *testing.T, fixture controlFixture) (int64, int64) {
	t.Helper()
	var versions, events int64
	err := fixture.database.QueryRowContext(t.Context(), `SELECT (SELECT sum(version) FROM netplay_room_members WHERE room_id=?),(SELECT count(*) FROM netplay_events WHERE room_id=?)`, fixture.room.RoomID, fixture.room.RoomID).Scan(&versions, &events)
	if err != nil {
		t.Fatal(err)
	}
	return versions, events
}

func assertControlRollback(t *testing.T, action string) {
	t.Helper()
	fixture := newControlFixture(t)
	beforeVersions, beforeEvents := controlCounts(t, fixture)
	actor := "host"
	if action == "seat" {
		actor = "guest"
	}
	sentinel := errors.New("late control failure")
	want := sentinel
	if action == "stale room" || action == "stale member" {
		want = application.ErrPrecondition
	}
	err := repository.NewRoomControl(fixture.database).WithWrite(t.Context(), func(scope application.RoomControlScope) error {
		before, err := scope.Read.Current(t.Context(), fixture.room.RoomID, actor)
		if err != nil {
			return err
		}
		evidence := application.RoomControlEvidence{ActorID: actor, Now: fixture.now.UnixMilli(), ExpiresAtMS: fixture.now.Add(time.Hour).UnixMilli(), Data: []byte(`{"schemaVersion":1}`)}
		if err := applyControlTestWrite(t, scope, before, evidence, action); err != nil {
			return err
		}
		snapshot, err := scope.Read.Snapshot(t.Context(), fixture.room.RoomID)
		if err != nil {
			return err
		}
		if snapshot.Version != fixture.room.Version+1 {
			t.Fatalf("write was not visible inside transaction: %+v", snapshot)
		}
		return sentinel
	})
	if !errors.Is(err, want) {
		t.Fatalf("action %s error=%v want=%v", action, err, want)
	}
	after, err := fixture.service.Room(t.Context(), fixture.room.RoomID, "host")
	if err != nil {
		t.Fatal(err)
	}
	versions, events := controlCounts(t, fixture)
	if !reflect.DeepEqual(after, fixture.room) || versions != beforeVersions || events != beforeEvents {
		t.Fatalf("rollback changed room=%+v versions=%d/%d events=%d/%d", after, versions, beforeVersions, events, beforeEvents)
	}
}
