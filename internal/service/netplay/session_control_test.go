package netplay

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/netplay"
)

type sessionControlMemory struct {
	before                                   model.SessionControlSnapshot
	sessions                                 []model.SessionTransitionPlan
	peers                                    []model.PeerTransitionPlan
	readFailure, writeFailure, commitFailure error
}

func (memory *sessionControlMemory) withControl(
	_, _ string,
	work func(model.SessionControlSnapshot) error,
) error {
	if memory.readFailure != nil {
		return memory.readFailure
	}
	if err := work(memory.before); err != nil {
		return err
	}
	return memory.commitFailure
}

func (memory *sessionControlMemory) writeSession(plan model.SessionTransitionPlan) error {
	memory.sessions = append(memory.sessions, plan)
	return memory.writeFailure
}

func (memory *sessionControlMemory) writePeer(plan model.PeerTransitionPlan) error {
	memory.peers = append(memory.peers, plan)
	return memory.writeFailure
}

func (memory *sessionControlMemory) CommitSetSessionState(
	_ context.Context, cmd model.SetSessionStateCommand,
) error {
	return memory.withControl(cmd.RoomID, cmd.SessionID,
		func(before model.SessionControlSnapshot) error {
			if before.HostID != cmd.ActorID {
				return model.ErrForbidden
			}
			if cmd.Target == "PAUSED_RECONNECT" && before.State != "RUNNING" ||
				cmd.Target == "RUNNING" && before.State != "PAUSED_RECONNECT" {
				return model.ErrRoomConflict
			}
			kind := "PAUSED"
			if cmd.Target == "RUNNING" {
				kind = "RESUMED"
			}
			event := model.SessionEvent{
				Type: kind,
				Data: model.SessionEventData{
					SchemaVersion: 1,
					FromState:     before.State,
					ToState:       cmd.Target,
				},
			}
			event.ActorID = &cmd.ActorID
			player := 1
			event.PlayerNo = &player
			return memory.writeSession(model.SessionTransitionPlan{
				Before: before, Target: cmd.Target,
				Events: []model.SessionEvent{event}, Now: cmd.NowMS,
			})
		},
	)
}

func (memory *sessionControlMemory) CommitResync(
	_ context.Context, cmd model.ResyncCommand,
) error {
	return memory.withControl(cmd.RoomID, cmd.SessionID,
		func(before model.SessionControlSnapshot) error {
			if !model.ValidResyncSource(cmd.Cause, before.State) {
				return model.ErrRoomConflict
			}
			kind := "RESUMED"
			if cmd.Cause == model.ResyncHash {
				kind = "PAUSED"
			}
			event := model.SessionEvent{
				Type: kind,
				Data: model.SessionEventData{
					SchemaVersion: 1, FromState: before.State,
					ToState: "RESYNCHRONIZING", Reason: string(cmd.Cause),
				},
			}
			return memory.writeSession(model.SessionTransitionPlan{
				Before: before, Target: "RESYNCHRONIZING",
				IncrementResync: true, PeerMode: model.PeersPrepareResync,
				Events: []model.SessionEvent{event}, Now: cmd.NowMS,
			})
		},
	)
}

func (memory *sessionControlMemory) CommitRunning(
	_ context.Context, cmd model.RunningCommand,
) error {
	return memory.withControl(cmd.RoomID, cmd.SessionID,
		func(before model.SessionControlSnapshot) error {
			if before.State != "SYNCHRONIZING" && before.State != "RESYNCHRONIZING" {
				return model.ErrRoomConflict
			}
			events := []model.SessionEvent{{
				Type: "SESSION_STATE_CHANGED",
				Data: model.SessionEventData{
					SchemaVersion: 1, FromState: before.State, ToState: "RUNNING",
				},
			}}
			if before.State == "RESYNCHRONIZING" {
				events = append(events, model.SessionEvent{
					Type: "RESYNCED",
					Data: model.SessionEventData{
						SchemaVersion: 1, ResyncCount: &before.ResyncCount,
					},
				})
			}
			return memory.writeSession(model.SessionTransitionPlan{
				Before: before, Target: "RUNNING", Started: true,
				PeerMode: model.PeersConnect, Events: events, Now: cmd.NowMS,
			})
		},
	)
}

func (memory *sessionControlMemory) CommitDisconnected(
	_ context.Context, cmd model.DisconnectedCommand,
) error {
	return memory.withControl(cmd.Identity.RoomID, cmd.Identity.SessionID,
		func(before model.SessionControlSnapshot) error {
			peer, err := model.ControlPeer(before, cmd.Identity)
			if err != nil {
				return err
			}
			if peer.State != "CONNECTED" {
				return nil
			}
			lease := cmd.NowMS + cmd.LeaseMS
			if err := memory.writePeer(model.PeerTransitionPlan{
				Before: before, Peer: peer, Target: "DISCONNECTED",
				DisconnectedAtMS: &cmd.NowMS, LeaseExpiresAtMS: &lease,
				Now: cmd.NowMS,
			}); err != nil {
				return err
			}
			if before.State != "RUNNING" {
				return nil
			}
			event := model.SessionEvent{
				Type: "PAUSED",
				Data: model.SessionEventData{
					SchemaVersion: 1, FromState: "RUNNING",
					ToState: "PAUSED_RECONNECT", Reason: "PEER_DISCONNECTED",
				},
			}
			event.ActorID = &cmd.Identity.ProfileID
			event.PlayerNo = &cmd.Identity.PlayerNo
			return memory.writeSession(model.SessionTransitionPlan{
				Before: before, Target: "PAUSED_RECONNECT",
				Events: []model.SessionEvent{event}, Now: cmd.NowMS,
			})
		},
	)
}

func (memory *sessionControlMemory) CommitRuntimeReady(
	_ context.Context, cmd model.RuntimeReadyCommand,
) (bool, error) {
	allReady := false
	err := memory.withControl(cmd.Identity.RoomID, cmd.Identity.SessionID,
		func(before model.SessionControlSnapshot) error {
			peer, err := model.ControlPeer(before, cmd.Identity)
			if err != nil {
				return err
			}
			if peer.State == "LAUNCH_READY" {
				event := model.SessionEvent{
					Type: "PARTICIPANT_STATE_CHANGED",
					Data: model.SessionEventData{
						SchemaVersion: 1, FromState: "LAUNCH_READY",
						ToState: "RUNTIME_READY",
					},
				}
				event.ActorID = &cmd.Identity.ProfileID
				event.PlayerNo = &cmd.Identity.PlayerNo
				if err := memory.writePeer(model.PeerTransitionPlan{
					Before: before, Peer: peer, Target: "RUNTIME_READY",
					Events: []model.SessionEvent{event}, Now: cmd.NowMS,
				}); err != nil {
					return err
				}
				peer.State = "RUNTIME_READY"
			}
			allReady = before.State == "LOADING" && model.AllControlPeersReady(before, peer)
			if !allReady {
				return nil
			}
			event := model.SessionEvent{
				Type: "SESSION_STATE_CHANGED",
				Data: model.SessionEventData{
					SchemaVersion: 1, FromState: "LOADING", ToState: "SYNCHRONIZING",
				},
			}
			return memory.writeSession(model.SessionTransitionPlan{
				Before: before, Target: "SYNCHRONIZING",
				Events: []model.SessionEvent{event}, Now: cmd.NowMS,
			})
		},
	)
	return allReady, err
}

func sessionControlFixture() (*SessionControl, *sessionControlMemory, model.PeerIdentity) {
	memory := &sessionControlMemory{before: model.SessionControlSnapshot{
		RoomID: "room", SessionID: "session", HostID: "host",
		RoomState: model.RoomStateRunning, RoomVersion: 7,
		State: "RUNNING", Version: 3,
		Peers: []model.SessionPeer{
			{
				ProfileID: "host", PlayerNo: 1, CredentialGeneration: 2,
				State: "CONNECTED", Version: 4,
			},
			{
				ProfileID: "guest", PlayerNo: 2, CredentialGeneration: 5,
				State: "CONNECTED", Version: 6,
			},
		},
	}}
	service := NewSessionControl(memory, 10*time.Second,
		func() time.Time { return time.UnixMilli(1786000000000) })
	peer := model.PeerIdentity{
		RoomID: "room", SessionID: "session",
		ProfileID: "guest", PlayerNo: 2, CredentialGeneration: 5,
	}
	return service, memory, peer
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
			if err := service.Disconnected(t.Context(), peer); !errors.Is(err, model.ErrForbidden) {
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
	if plan.Target != "DISCONNECTED" || plan.LeaseExpiresAtMS == nil ||
		*plan.LeaseExpiresAtMS != 1786000010000 ||
		plan.Peer.Version != 6 || memory.sessions[0].Target != "PAUSED_RECONNECT" {
		t.Fatalf("disconnect plan=%+v", plan)
	}
	memory.before.Peers[1].State = "DISCONNECTED"
	memory.before.State = "PAUSED_RECONNECT"
	memory.sessions = nil
	memory.peers = nil
	if err := service.Disconnected(t.Context(), peer); err != nil {
		t.Fatal(err)
	}
	if len(memory.peers) != 0 || len(memory.sessions) != 0 {
		t.Fatal("duplicate disconnect wrote again")
	}
}

func TestSessionControlRuntimeReadyUsesOccupiedParticipants(t *testing.T) {
	t.Parallel()
	service, memory, peer := sessionControlFixture()
	memory.before.RoomState = model.RoomStateStarting
	memory.before.State = "LOADING"
	memory.before.Peers[0].State = "RUNTIME_READY"
	memory.before.Peers[1].State = "LAUNCH_READY"
	all, err := service.RuntimeReady(t.Context(), peer)
	if err != nil || !all || len(memory.sessions) != 1 ||
		memory.sessions[0].Target != "SYNCHRONIZING" ||
		memory.peers[0].Target != "RUNTIME_READY" {
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
	for _, cause := range []model.ResyncCause{
		model.ResyncReconnect, model.ResyncHash, model.ResyncHost,
	} {
		t.Run(string(cause), func(t *testing.T) {
			service, memory, _ := sessionControlFixture()
			if cause == model.ResyncHost {
				memory.before.State = "PAUSED_RECONNECT"
			}
			if err := service.Resync(t.Context(), "room", "session", cause); err != nil {
				t.Fatal(err)
			}
			plan := memory.sessions[0]
			if plan.Target != "RESYNCHRONIZING" || !plan.IncrementResync ||
				plan.PeerMode != model.PeersPrepareResync {
				t.Fatalf("resync=%+v", plan)
			}
			memory.before.State = "RESYNCHRONIZING"
			memory.before.ResyncCount = 2
			memory.sessions = nil
			if err := service.Running(t.Context(), "room", "session"); err != nil {
				t.Fatal(err)
			}
			plan = memory.sessions[0]
			if plan.Target != "RUNNING" || !plan.Started ||
				plan.PeerMode != model.PeersConnect || len(plan.Events) != 2 {
				t.Fatalf("running=%+v", plan)
			}
		})
	}
}

func TestSessionControlHostAndSourceStateGuards(t *testing.T) {
	t.Parallel()
	service, memory, _ := sessionControlFixture()
	if err := service.SetState(t.Context(), "room", "session", "guest",
		"PAUSED_RECONNECT"); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("guest pause=%v", err)
	}
	if err := service.SetState(t.Context(), "room", "session", "host",
		"RUNNING"); !errors.Is(err, model.ErrRoomConflict) {
		t.Fatalf("invalid resume=%v", err)
	}
	if err := service.Resync(t.Context(), "room", "session",
		model.ResyncHost); !errors.Is(err, model.ErrRoomConflict) {
		t.Fatalf("invalid resync=%v", err)
	}
	if len(memory.sessions) != 0 {
		t.Fatal("invalid transitions written")
	}
	if err := service.SetState(t.Context(), "room", "session", "host",
		"PAUSED_RECONNECT"); err != nil {
		t.Fatal(err)
	}
	if len(memory.sessions) != 1 || memory.sessions[0].Events[0].Type != "PAUSED" {
		t.Fatalf("pause plan=%v", memory.sessions)
	}
}
