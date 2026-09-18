package netplay

import (
	"errors"
	"reflect"
	"testing"

	application "retrom/internal/model/netplay"
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

	repo := repository.NewParticipantPreparation(fixture.database)
	snapshot, err := repo.Snapshot(t.Context(), peer.RoomID, peer.SessionID, peer.ProfileID)
	if err != nil {
		t.Fatal(err)
	}
	switch action {
	case "stale room":
		snapshot.Control.RoomVersion++
	case "stale peer":
		snapshot.Peer.CredentialGeneration++
	}
	events := []application.SessionEvent{{Type: "PARTICIPANT_STATE_CHANGED", ActorID: &peer.ProfileID, PlayerNo: &peer.PlayerNo, Data: application.SessionEventData{SchemaVersion: 1, FromState: "LOCKED", ToState: "LAUNCH_READY"}}}
	advance := action == "loading"
	if advance {
		events = append(events, application.SessionEvent{Type: "SESSION_STATE_CHANGED", Data: application.SessionEventData{SchemaVersion: 1, FromState: "PREPARING", ToState: "LOADING"}})
	}

	sentinel := errors.New("late preparation commit failure")
	if action != "stale room" && action != "stale peer" {
		repo.WithPreCommitHook(func() error { return sentinel })
	}
	commitErr := repo.CommitPreparation(t.Context(), application.PreparationPlan{Before: snapshot, Events: events, AdvanceLoading: advance, Now: fixture.now.UnixMilli()})
	want := sentinel
	if action == "stale room" || action == "stale peer" {
		want = ErrRoomConflict
	}
	if !errors.Is(commitErr, want) {
		t.Fatalf("record failure=%v", commitErr)
	}
	if after := sessionControlRecordsSnapshot(t, fixture); !reflect.DeepEqual(before, after) {
		t.Fatalf("record rollback before=%v after=%v", before, after)
	}
}
