package netplay

import (
	"context"

	model "retrom/internal/model/netplay"
)

func controlPeer(before model.SessionControlSnapshot, identity model.PeerIdentity) (model.SessionPeer, error) {
	for _, peer := range before.Peers {
		if peer.ProfileID == identity.ProfileID && peer.PlayerNo == identity.PlayerNo &&
			peer.CredentialGeneration == identity.CredentialGeneration {
			return peer, nil
		}
	}
	return model.SessionPeer{}, model.ErrForbidden
}

func (service *SessionControl) Disconnected(ctx context.Context, identity model.PeerIdentity) error {
	return service.mutate(
		ctx,
		identity.RoomID,
		identity.SessionID,
		func(scope model.SessionControlScope, before model.SessionControlSnapshot, now int64) error {
			peer, err := controlPeer(before, identity)
			if err != nil {
				return err
			}
			if peer.State != "CONNECTED" {
				return nil
			}
			lease := now + service.reconnectLease.Milliseconds()
			if err := writePeerControl(
				ctx,
				scope.Write, model.PeerTransitionPlan{
					Before:           before,
					Peer:             peer,
					Target:           "DISCONNECTED",
					DisconnectedAtMS: &now,
					LeaseExpiresAtMS: &lease,
					Now:              now,
				},
			); err != nil {
				return err
			}
			if before.State != "RUNNING" {
				return nil
			}
			event := stateEvent("PAUSED", "RUNNING", "PAUSED_RECONNECT", "PEER_DISCONNECTED")
			event.ActorID = &identity.ProfileID
			event.PlayerNo = &identity.PlayerNo
			return writeSessionControl(
				ctx,
				scope.Write, model.SessionTransitionPlan{
					Before: before,
					Target: "PAUSED_RECONNECT",
					Events: []model.SessionEvent{event},
					Now:    now,
				},
			)
		},
	)
}

func (service *SessionControl) RuntimeReady(ctx context.Context, identity model.PeerIdentity) (bool, error) {
	allReady := false
	err := service.mutate(
		ctx,
		identity.RoomID,
		identity.SessionID,
		func(scope model.SessionControlScope, before model.SessionControlSnapshot, now int64) error {
			peer, err := controlPeer(before, identity)
			if err != nil {
				return err
			}
			if peer.State == "LAUNCH_READY" {
				event := stateEvent("PARTICIPANT_STATE_CHANGED", "LAUNCH_READY", "RUNTIME_READY", "")
				event.ActorID = &identity.ProfileID
				event.PlayerNo = &identity.PlayerNo
				if err := writePeerControl(
					ctx,
					scope.Write, model.PeerTransitionPlan{
						Before: before,
						Peer:   peer,
						Target: "RUNTIME_READY",
						Events: []model.SessionEvent{event},
						Now:    now,
					},
				); err != nil {
					return err
				}
				peer.State = "RUNTIME_READY"
			}
			allReady = before.State == "LOADING" && allControlPeersReady(before, peer)
			if !allReady {
				return nil
			}
			event := stateEvent("SESSION_STATE_CHANGED", "LOADING", "SYNCHRONIZING", "")
			return writeSessionControl(
				ctx,
				scope.Write, model.SessionTransitionPlan{
					Before: before,
					Target: "SYNCHRONIZING",
					Events: []model.SessionEvent{event},
					Now:    now,
				},
			)
		},
	)
	if err != nil {
		return false, err
	}
	return allReady, nil
}

func allControlPeersReady(before model.SessionControlSnapshot, changed model.SessionPeer) bool {
	if len(before.Peers) < 2 {
		return false
	}
	for _, peer := range before.Peers {
		if peer.ProfileID == changed.ProfileID {
			peer = changed
		}
		if peer.State != "RUNTIME_READY" {
			return false
		}
	}
	return true
}
