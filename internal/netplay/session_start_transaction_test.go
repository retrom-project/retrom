package netplay

import (
	"context"
	"errors"
	"reflect"
	"testing"

	repository "retrom/internal/persistence/netplay"
	application "retrom/internal/service/netplay"
)

type failedSessionStart struct {
	repository application.SessionStartRepository
	failure    error
	stale      bool
}

func (wrapper failedSessionStart) WithStart(ctx context.Context, work func(application.SessionStartScope) error) error {
	return wrapper.repository.WithStart(ctx, func(scope application.SessionStartScope) error {
		if wrapper.stale {
			scope.Write = staleSessionStartWriter{scope.Write}
		}
		if err := work(scope); err != nil {
			return err
		}
		return wrapper.failure
	})
}

type staleSessionStartWriter struct{ application.SessionStartWriter }

func (writer staleSessionStartWriter) Insert(ctx context.Context, plan application.SessionStartPlan) (application.Room, error) {
	plan.Before.Version++
	return writer.SessionStartWriter.Insert(ctx, plan)
}

func TestSessionStartRollsBackSnapshotParticipantsAndEvent(t *testing.T) {
	t.Parallel()
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "late failure", true: "stale version"}[stale], func(t *testing.T) {
			assertFailedStartRollback(t, stale)
		})
	}
}

func readyControlFixture(t *testing.T) controlFixture {
	t.Helper()
	fixture := newControlFixture(t)
	room, err := fixture.service.SetReady(t.Context(), fixture.room.RoomID, "host", true, fixture.room.Version)
	if err != nil {
		t.Fatal(err)
	}
	room, err = fixture.service.SetReady(t.Context(), room.RoomID, "guest", true, room.Version)
	if err != nil {
		t.Fatal(err)
	}
	fixture.room, err = fixture.service.Room(t.Context(), room.RoomID, "host")
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func assertFailedStartRollback(t *testing.T, stale bool) {
	t.Helper()
	fixture := readyControlFixture(t)
	beforeVersions, beforeEvents := controlCounts(t, fixture)
	sentinel := errors.New("late start failure")
	want := sentinel
	if stale {
		want = ErrPrecondition
	}
	wrapped := failedSessionStart{repository: repository.NewSessionStart(fixture.database), failure: sentinel, stale: stale}
	starter := application.NewSessionStart(wrapped, fixture.service.registry, fixture.service.clock.Now)
	result, err := starter.Start(t.Context(), fixture.room.RoomID, "host", fixture.room.Version)
	if !errors.Is(err, want) || result.RoomID != "" || result.CurrentSession != nil {
		t.Fatalf("failed start result=%+v error=%v", result, err)
	}
	for _, query := range []string{`SELECT count(*) FROM netplay_sessions`, `SELECT count(*) FROM netplay_session_participants`} {
		var count int
		if err := fixture.database.QueryRowContext(t.Context(), query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("failed start retained %d records", count)
		}
	}
	after, err := fixture.service.Room(t.Context(), fixture.room.RoomID, "host")
	if err != nil {
		t.Fatal(err)
	}
	versions, events := controlCounts(t, fixture)
	if !reflect.DeepEqual(after, fixture.room) || versions != beforeVersions || events != beforeEvents {
		t.Fatalf("failed start changed room=%+v versions=%d/%d events=%d/%d", after, versions, beforeVersions, events, beforeEvents)
	}
}
