package netplay

import (
	"context"
	"errors"
	"testing"
	"time"
)

type sessionControlMemory struct {
	before                                   SessionControlSnapshot
	sessions                                 []SessionTransitionPlan
	peers                                    []PeerTransitionPlan
	readFailure, writeFailure, commitFailure error
}

func (memory *sessionControlMemory) WithControl(_ context.Context, work func(SessionControlScope) error) error {
	if err := work(SessionControlScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	return memory.commitFailure
}

func (memory *sessionControlMemory) Current(context.Context, string, string) (SessionControlSnapshot, error) {
	return memory.before, memory.readFailure
}

func (memory *sessionControlMemory) Session(_ context.Context, plan SessionTransitionPlan) error {
	memory.sessions = append(memory.sessions, plan)
	return memory.writeFailure
}

func (memory *sessionControlMemory) Peer(_ context.Context, plan PeerTransitionPlan) error {
	memory.peers = append(memory.peers, plan)
	return memory.writeFailure
}

func sessionControlFixture() (*SessionControl, *sessionControlMemory, PeerIdentity) {
	memory := &sessionControlMemory{before: SessionControlSnapshot{RoomID: "room", SessionID: "session", HostID: "host", RoomState: RoomStateRunning, RoomVersion: 7, State: "RUNNING", Version: 3, Peers: []SessionPeer{{ProfileID: "host", PlayerNo: 1, CredentialGeneration: 2, State: "CONNECTED", Version: 4}, {ProfileID: "guest", PlayerNo: 2, CredentialGeneration: 5, State: "CONNECTED", Version: 6}}}}
	service := NewSessionControl(memory, 10*time.Second, func() time.Time { return time.UnixMilli(1786000000000) })
	return service, memory, PeerIdentity{RoomID: "room", SessionID: "session", ProfileID: "guest", PlayerNo: 2, CredentialGeneration: 5}
}

func TestSessionControlDisconnectRejectsStaleIdentity(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"profile", "generation", "seat"} {
		t.Run(field, func(t *testing.T) {
			service, memory, peer := sessionControlFixture()
			switch field {
			case "profile":
				peer.ProfileID = "outsider"
			case "generation":
				peer.CredentialGeneration--
			case "seat":
				peer.PlayerNo = 1
			}
			if err := service.Disconnected(t.Context(), peer); !errors.Is(err, ErrForbidden) {
				t.Fatalf("disconnect=%v", err)
			}
			if len(memory.sessions) != 0 || len(memory.peers) != 0 {
				t.Fatal("stale identity wrote records")
			}
		})
	}
}

func TestSessionControlDisconnectPausesOnceAndSetsLease(t *testing.T) {
	t.Parallel()
	service, memory, peer := sessionControlFixture()
	if err := service.Disconnected(t.Context(), peer); err != nil {
		t.Fatal(err)
	}
	if len(memory.peers) != 1 || len(memory.sessions) != 1 {
		t.Fatalf("peers=%v sessions=%v", memory.peers, memory.sessions)
	}
	plan := memory.peers[0]
	if plan.Target != "DISCONNECTED" || plan.LeaseExpiresAtMS == nil || *plan.LeaseExpiresAtMS != 1786000010000 || plan.Peer.Version != 6 || memory.sessions[0].Target != "PAUSED_RECONNECT" {
		t.Fatalf("disconnect plan=%+v", plan)
	}
	memory.before.Peers[1].State = "DISCONNECTED"
	memory.before.State = "PAUSED_RECONNECT"
	if err := service.Disconnected(t.Context(), peer); err != nil {
		t.Fatal(err)
	}
	if len(memory.peers) != 1 || len(memory.sessions) != 1 {
		t.Fatal("duplicate disconnect wrote again")
	}
}

func TestSessionControlRuntimeReadyUsesOccupiedParticipants(t *testing.T) {
	t.Parallel()
	service, memory, peer := sessionControlFixture()
	memory.before.RoomState = RoomStateStarting
	memory.before.State = "LOADING"
	memory.before.Peers[0].State = "RUNTIME_READY"
	memory.before.Peers[1].State = "LAUNCH_READY"
	all, err := service.RuntimeReady(t.Context(), peer)
	if err != nil || !all || len(memory.sessions) != 1 || memory.sessions[0].Target != "SYNCHRONIZING" || memory.peers[0].Target != "RUNTIME_READY" {
		t.Fatalf("ready=%v error=%v writes=%v", all, err, memory.sessions)
	}
	sentinel := errors.New("commit failed")
	memory.commitFailure = sentinel
	all, err = service.RuntimeReady(t.Context(), peer)
	if all || !errors.Is(err, sentinel) {
		t.Fatalf("failed commit ready=%v error=%v", all, err)
	}
}

func TestSessionControlResyncAndRunPlans(t *testing.T) {
	t.Parallel()
	for _, cause := range []ResyncCause{ResyncReconnect, ResyncHash, ResyncHost} {
		t.Run(string(cause), func(t *testing.T) {
			service, memory, _ := sessionControlFixture()
			if cause == ResyncHost {
				memory.before.State = "PAUSED_RECONNECT"
			}
			if err := service.Resync(t.Context(), "room", "session", cause); err != nil {
				t.Fatal(err)
			}
			plan := memory.sessions[0]
			if plan.Target != "RESYNCHRONIZING" || !plan.IncrementResync || plan.PeerMode != PeersPrepareResync {
				t.Fatalf("resync=%+v", plan)
			}
			memory.before.State = "RESYNCHRONIZING"
			memory.before.ResyncCount = 2
			if err := service.Running(t.Context(), "room", "session"); err != nil {
				t.Fatal(err)
			}
			plan = memory.sessions[1]
			if plan.Target != "RUNNING" || !plan.Started || plan.PeerMode != PeersConnect || len(plan.Events) != 2 {
				t.Fatalf("running=%+v", plan)
			}
		})
	}
}

func TestSessionControlHostAndSourceStateGuards(t *testing.T) {
	t.Parallel()
	service, memory, _ := sessionControlFixture()
	if err := service.SetState(t.Context(), "room", "session", "guest", "PAUSED_RECONNECT"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("guest pause=%v", err)
	}
	if err := service.SetState(t.Context(), "room", "session", "host", "RUNNING"); !errors.Is(err, ErrRoomConflict) {
		t.Fatalf("invalid resume=%v", err)
	}
	if err := service.Resync(t.Context(), "room", "session", ResyncHost); !errors.Is(err, ErrRoomConflict) {
		t.Fatalf("invalid resync=%v", err)
	}
	if len(memory.sessions) != 0 {
		t.Fatal("invalid transitions written")
	}
	if err := service.SetState(t.Context(), "room", "session", "host", "PAUSED_RECONNECT"); err != nil {
		t.Fatal(err)
	}
	if len(memory.sessions) != 1 || memory.sessions[0].Events[0].Type != "PAUSED" {
		t.Fatalf("pause plan=%v", memory.sessions)
	}
}
