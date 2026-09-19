package netplay

import (
	"errors"
	"reflect"
	"testing"

	netplaymodel "retrom/internal/model/netplay"
	repository "retrom/internal/repo/netplay"
)

func TestParticipantPreparationRecordRollsBackEventsAndLoading(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"event", "loading", "stale room", "stale peer"} {
		t.Run(action, func(t *testing.T) { assertPreparationRecordRollback(t, action) })
	}
}

func assertPreparationRecordRollback(t *testing.T, action string) {
	t.Helper()
	fixture, peer := controlledSessionFixture(t, "PREPARING")
	before := sessionControlRecordsSnapshot(t, fixture)
	sentinel := errors.New("late preparation commit failure")
	err := repository.NewParticipantPreparation(fixture.database).WithPreparation(t.Context(), func(scope netplaymodel.PreparationScope) error {
		snapshot, err := scope.Read.Snapshot(t.Context(), peer.RoomID, peer.SessionID, peer.ProfileID)
		if err != nil {
			return err
		}
		switch action {
		case "stale room":
			snapshot.Control.RoomVersion++
		case "stale peer":
			snapshot.Peer.CredentialGeneration++
		}
		events := []netplaymodel.SessionEvent{{Type: "PARTICIPANT_STATE_CHANGED", ActorID: &peer.ProfileID, PlayerNo: &peer.PlayerNo, Data: netplaymodel.SessionEventData{SchemaVersion: 1, FromState: "LOCKED", ToState: "LAUNCH_READY"}}}
		advance := action == "loading"
		if advance {
			events = append(events, netplaymodel.SessionEvent{Type: "SESSION_STATE_CHANGED", Data: netplaymodel.SessionEventData{SchemaVersion: 1, FromState: "PREPARING", ToState: "LOADING"}})
		}
		if err := scope.Write.Record(t.Context(), netplaymodel.PreparationPlan{Before: snapshot, Events: events, AdvanceLoading: advance, Now: fixture.now.UnixMilli()}); err != nil {
			return err
		}
		return sentinel
	})
	want := sentinel
	if action == "stale room" || action == "stale peer" {
		want = ErrRoomConflict
	}
	if !errors.Is(err, want) {
		t.Fatalf("record failure=%v", err)
	}
	if after := sessionControlRecordsSnapshot(t, fixture); !reflect.DeepEqual(before, after) {
		t.Fatalf("record rollback before=%v after=%v", before, after)
	}
}
