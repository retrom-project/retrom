package netplay

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	repository "retrom/internal/persistence/netplay"
	application "retrom/internal/service/netplay"
)

type failedRoomMaintenance struct {
	application.MaintenanceRepository
	failure error
}

func (wrapper failedRoomMaintenance) WithMaintenance(ctx context.Context, work func(application.MaintenanceWriter) error) error {
	return wrapper.MaintenanceRepository.WithMaintenance(ctx, func(writer application.MaintenanceWriter) error {
		if err := work(writer); err != nil {
			return err
		}
		return wrapper.failure
	})
}

func TestRoomMaintenanceRecoveryRollbackIncludesLaunchesAndPlays(t *testing.T) {
	t.Parallel()
	fixture, peer := controlledSessionFixture(t, "RUNNING")
	if _, err := fixture.database.ExecContext(t.Context(), `
INSERT INTO play_sessions(id,launch_session_id,profile_id,game_id,started_at_ms,last_heartbeat_at_ms,
active_duration_ms,last_client_sequence,state,version,created_at_ms,updated_at_ms)
SELECT 'play-'||profile_id,id,profile_id,game_id,?,?,0,0,'ACTIVE',1,?,? FROM launch_sessions`, fixture.now.UnixMilli(), fixture.now.UnixMilli(), fixture.now.UnixMilli(), fixture.now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	before := maintenanceRecoverySnapshot(t, fixture)
	sentinel := errors.New("late recovery failure")
	service := application.NewRoomMaintenance(failedRoomMaintenance{repository.NewRoomMaintenance(fixture.database), sentinel}, nil, func() time.Time { return fixture.now })
	if err := service.Recover(t.Context(), "SERVER_RESTARTED"); !errors.Is(err, sentinel) {
		t.Fatalf("recovery failure=%v", err)
	}
	if after := maintenanceRecoverySnapshot(t, fixture); !reflect.DeepEqual(before, after) {
		t.Fatalf("recovery rollback before=%v after=%v", before, after)
	}
	if err := fixture.service.Recover(t.Context(), "SERVER_RESTARTED"); err != nil {
		t.Fatal(err)
	}
	assertRecoveredRuntime(t, fixture, peer.SessionID)
}

func maintenanceRecoverySnapshot(t *testing.T, fixture controlFixture) []string {
	t.Helper()
	result := roomExitRecordsSnapshot(t, fixture)
	result = append(result, readExitRecords(t, fixture, `SELECT json_object('id',id,'state',state,'version',version,'finish',finished_at_ms) FROM launch_sessions ORDER BY id`)...)
	return append(result, readExitRecords(t, fixture, `SELECT json_object('id',id,'state',state,'version',version,'end',ended_at_ms) FROM play_sessions ORDER BY id`)...)
}

func assertRecoveredRuntime(t *testing.T, fixture controlFixture, sessionID string) {
	t.Helper()
	var state string
	var active int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT state FROM netplay_sessions WHERE id=?`, sessionID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM launch_sessions WHERE state IN ('CREATED','ACTIVE'))+(SELECT count(*) FROM play_sessions WHERE state='ACTIVE')`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if state != "FAILED" || active != 0 {
		t.Fatalf("recovered state=%s active launches/plays=%d", state, active)
	}
}

func TestPassiveRoomExpiryFencesVersionsAndRollsBackEvent(t *testing.T) {
	t.Parallel()
	fixture := newControlFixture(t)
	now := fixture.now.Add(2 * time.Hour)
	repo := repository.NewRoomMaintenance(fixture.database)
	candidates, err := repo.Passive(t.Context(), application.ExpiryCutoffs{Now: now.UnixMilli(), Limit: 100})
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates=%v error=%v", candidates, err)
	}
	before := roomExitRecordsSnapshot(t, fixture)
	candidate := candidates[0]
	candidate.Version++
	err = repo.WithMaintenance(t.Context(), func(writer application.MaintenanceWriter) error {
		return writer.Expire(t.Context(), application.ExpiryPlan{Before: candidate, Now: now.UnixMilli()})
	})
	if err != nil {
		t.Fatal(err)
	}
	if after := roomExitRecordsSnapshot(t, fixture); !reflect.DeepEqual(before, after) {
		t.Fatal("stale passive expiry changed room")
	}
	sentinel := errors.New("late expiry failure")
	wrapper := failedRoomMaintenance{repo, sentinel}
	err = wrapper.WithMaintenance(t.Context(), func(writer application.MaintenanceWriter) error {
		return writer.Expire(t.Context(), application.ExpiryPlan{Before: candidates[0], Now: now.UnixMilli()})
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expiry failure=%v", err)
	}
	if after := roomExitRecordsSnapshot(t, fixture); !reflect.DeepEqual(before, after) {
		t.Fatal("late expiry retained room or event")
	}
}

func TestActiveRoomExpirySkipsReplacedVersions(t *testing.T) {
	t.Parallel()
	fixture, peer := controlledSessionFixture(t, "PREPARING")
	now := fixture.now.Add(3 * time.Minute)
	repo := repository.NewRoomMaintenance(fixture.database)
	candidates, err := repo.Active(t.Context(), application.ExpiryCutoffs{StartingBefore: now.Add(-2 * time.Minute).UnixMilli(), RunningBefore: now.Add(-8 * time.Hour).UnixMilli(), Limit: 100})
	if err != nil || len(candidates) != 1 {
		t.Fatalf("active=%v error=%v", candidates, err)
	}
	before := roomExitRecordsSnapshot(t, fixture)
	candidate := candidates[0]
	candidate.Version--
	exit := application.NewRoomExit(repository.NewRoomExit(fixture.database), time.Hour, func() time.Time { return now })
	if err := exit.EndExpired(t.Context(), candidate, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if after := roomExitRecordsSnapshot(t, fixture); !reflect.DeepEqual(before, after) {
		t.Fatal("stale active expiry changed session")
	}
	if err := exit.EndExpired(t.Context(), candidates[0], now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	var state, reason string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT state,end_reason FROM netplay_sessions WHERE id=?`, peer.SessionID).Scan(&state, &reason); err != nil {
		t.Fatal(err)
	}
	room, err := fixture.service.Room(t.Context(), fixture.room.RoomID, "host")
	if err != nil || room.State != RoomStateWaiting || state != "FAILED" || reason != "START_TIMEOUT" {
		t.Fatalf("expired session=%s/%s room=%+v error=%v", state, reason, room, err)
	}
}
