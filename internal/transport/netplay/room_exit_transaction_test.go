package netplay

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	netplaymodel "retrom/internal/model/netplay"
	repository "retrom/internal/repo/netplay"
	netplayservice "retrom/internal/service/netplay"
)

func TestRoomExitRollsBackWholeTransaction(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"close", "end", "active leave", "waiting leave", "kick", "stale end", "stale room", "stale member"} {
		t.Run(action, func(t *testing.T) { assertRoomExitRollback(t, action) })
	}
}

func assertRoomExitRollback(t *testing.T, action string) {
	t.Helper()
	fixture := readyControlFixture(t)
	if action == "close" || action == "end" || action == "active leave" || action == "stale end" {
		room, err := fixture.service.Start(t.Context(), fixture.room.RoomID, "host", fixture.room.Version)
		if err != nil {
			t.Fatal(err)
		}
		fixture.room = room
	}
	before := roomExitRecordsSnapshot(t, fixture)

	sentinel := errors.New("late exit failure")
	stale := ""
	switch action {
	case "stale end", "stale room":
		stale = "room"
	case "stale member":
		stale = "member"
	}

	repo := repository.NewRoomExit(fixture.database)
	var exitRepo netplaymodel.RoomExitRepository = repo
	if stale != "" {
		exitRepo = staleRoomExitRepo{RoomExit: repo, stale: stale}
	} else {
		repo.WithPreCommitHook(func() error { return sentinel })
	}
	service := netplayservice.NewRoomExit(exitRepo, time.Hour, func() time.Time { return fixture.now })

	var err error
	switch action {
	case "close", "stale end":
		err = service.End(t.Context(), fixture.room.RoomID, "host", "HOST_CLOSED", &fixture.room.Version)
	case "end":
		err = service.End(t.Context(), fixture.room.RoomID, "guest", "USER_EXIT", nil)
	case "active leave", "waiting leave":
		err = service.Leave(t.Context(), fixture.room.RoomID, "guest", fixture.room.Version)
	default:
		err = service.Kick(t.Context(), fixture.room.RoomID, "host", fixture.room.Members[1].MemberID, fixture.room.Version)
	}
	want := sentinel
	if stale != "" {
		want = ErrPrecondition
	}
	if !errors.Is(err, want) {
		t.Fatalf("failed exit error=%v", err)
	}
	after := roomExitRecordsSnapshot(t, fixture)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("exit rollback before=%+v after=%+v", before, after)
	}
}

type staleRoomExitRepo struct {
	*repository.RoomExit
	stale string
}

func (s staleRoomExitRepo) CommitRoomEnd(ctx context.Context, plan netplaymodel.RoomEndPlan) error {
	if s.stale == "room" {
		plan.Before.Room.Version++
	}
	return s.RoomExit.CommitRoomEnd(ctx, plan)
}

func (s staleRoomExitRepo) CommitRoomRemoval(ctx context.Context, plan netplaymodel.RoomRemovalPlan) error {
	if s.stale == "room" {
		plan.Before.Version++
	}
	if s.stale == "member" {
		plan.Member.Version++
	}
	return s.RoomExit.CommitRoomRemoval(ctx, plan)
}

func roomExitRecordsSnapshot(t *testing.T, fixture controlFixture) []string {
	t.Helper()
	queries := []string{
		`SELECT json_object('state',state,'version',version,'session',current_session_id,'end',end_reason,'expiry',expires_at_ms) FROM netplay_rooms ORDER BY id`,
		`SELECT json_object('id',id,'ready',ready,'version',version,'left',left_at_ms,'reason',leave_reason) FROM netplay_room_members ORDER BY id`,
		`SELECT json_object('id',id,'state',state,'version',version,'reason',end_reason,'finish',finished_at_ms) FROM netplay_sessions ORDER BY id`,
		`SELECT json_object('profile',profile_id,'state',state,'version',version,'disconnect',disconnected_at_ms,'lease',lease_expires_at_ms) FROM netplay_session_participants ORDER BY profile_id`,
		`SELECT json_object('id',id,'type',event_type,'data',data_json) FROM netplay_events ORDER BY id`,
	}

	result := make([]string, 0, len(queries))
	for _, query := range queries {
		result = append(result, readExitRecords(t, fixture, query)...)
	}
	return result
}

func readExitRecords(t *testing.T, fixture controlFixture, query string) []string {
	t.Helper()
	rows, err := fixture.database.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Error(err)
		}
	}()
	var result []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}
