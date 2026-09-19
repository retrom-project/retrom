package netplay

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"testing"

	netplaymodel "retrom/internal/model/netplay"
	validationrepository "retrom/internal/repo/corevalidation"
	repository "retrom/internal/repo/netplay"
	"retrom/internal/service/corevalidation"
	netplayservice "retrom/internal/service/netplay"
	"retrom/internal/testkit/testsupport"
)

type staleSessionStart struct {
	repository netplaymodel.SessionStartRepository
}

func (wrapper staleSessionStart) InspectRoom(ctx context.Context, roomID, hostID string) (netplaymodel.RoomControlSnapshot, error) {
	return wrapper.repository.InspectRoom(ctx, roomID, hostID)
}

func (wrapper staleSessionStart) CommitSessionStart(ctx context.Context, cmd netplaymodel.SessionStartCommand) (netplaymodel.Room, error) {
	cmd.ExpectedVersion++
	return wrapper.repository.CommitSessionStart(ctx, cmd)
}

func TestSessionStartRollsBackSnapshotParticipantsAndEvent(t *testing.T) {
	t.Parallel()
	t.Run("late failure", func(t *testing.T) {
		assertFailedStartRollbackCommitFault(t)
	})
	t.Run("stale version", func(t *testing.T) {
		assertFailedStartRollbackStale(t)
	})
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

func assertFailedStartRollbackCommitFault(t *testing.T) {
	t.Helper()
	fixture := readyControlFixture(t)
	beforeVersions, beforeEvents := controlCounts(t, fixture)
	sentinel := errors.New("injected commit failure")
	faultDB := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if query == "COMMIT" {
				return sentinel
			}
			return nil
		},
	})
	faultRepo := repository.NewSessionStart(faultDB)
	eligibility := repository.NewEligibility(fixture.database)
	bios := corevalidation.New(validationrepository.New(fixture.database))
	starter := netplayservice.NewSessionStart(faultRepo, eligibility, bios, fixture.service.registry, fixture.service.clock.Now)
	result, err := starter.Start(t.Context(), fixture.room.RoomID, "host", fixture.room.Version)
	if !errors.Is(err, sentinel) || result.RoomID != "" || result.CurrentSession != nil {
		t.Fatalf("failed start result=%+v error=%v", result, err)
	}
	assertSessionStartNoRecords(t, fixture)
	after, err := fixture.service.Room(t.Context(), fixture.room.RoomID, "host")
	if err != nil {
		t.Fatal(err)
	}
	versions, events := controlCounts(t, fixture)
	if !reflect.DeepEqual(after, fixture.room) || versions != beforeVersions || events != beforeEvents {
		t.Fatalf("failed start changed room=%+v versions=%d/%d events=%d/%d", after, versions, beforeVersions, events, beforeEvents)
	}
}

func assertFailedStartRollbackStale(t *testing.T) {
	t.Helper()
	fixture := readyControlFixture(t)
	beforeVersions, beforeEvents := controlCounts(t, fixture)
	wrapped := staleSessionStart{repository: repository.NewSessionStart(fixture.database)}
	eligibility := repository.NewEligibility(fixture.database)
	bios := corevalidation.New(validationrepository.New(fixture.database))
	starter := netplayservice.NewSessionStart(wrapped, eligibility, bios, fixture.service.registry, fixture.service.clock.Now)
	result, err := starter.Start(t.Context(), fixture.room.RoomID, "host", fixture.room.Version)
	if !errors.Is(err, ErrPrecondition) || result.RoomID != "" || result.CurrentSession != nil {
		t.Fatalf("failed start result=%+v error=%v", result, err)
	}
	assertSessionStartNoRecords(t, fixture)
	after, err := fixture.service.Room(t.Context(), fixture.room.RoomID, "host")
	if err != nil {
		t.Fatal(err)
	}
	versions, events := controlCounts(t, fixture)
	if !reflect.DeepEqual(after, fixture.room) || versions != beforeVersions || events != beforeEvents {
		t.Fatalf("failed start changed room=%+v versions=%d/%d events=%d/%d", after, versions, beforeVersions, events, beforeEvents)
	}
}

func assertSessionStartNoRecords(t *testing.T, fixture controlFixture) {
	t.Helper()
	for _, query := range []string{`SELECT count(*) FROM netplay_sessions`, `SELECT count(*) FROM netplay_session_participants`} {
		var count int
		if err := fixture.database.QueryRowContext(t.Context(), query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("failed start retained %d records", count)
		}
	}
}
