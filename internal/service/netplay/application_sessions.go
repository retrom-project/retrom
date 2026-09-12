package netplay

import (
	"context"
)

func (service *Service) SetSessionState(ctx context.Context, roomID, sessionID, actorID, target string) error {
	if err := service.components.Sessions.SetState(ctx, roomID, sessionID, actorID, target); err != nil {
		return applicationError("set session state", err)
	}
	return nil
}

func (service *Service) prepareResync(ctx context.Context, roomID, sessionID string, cause ResyncCause) error {
	if err := service.components.Sessions.Resync(ctx, roomID, sessionID, cause); err != nil {
		return applicationError("prepare resync", err)
	}
	return nil
}

func (service *Service) PrepareReconnectResync(ctx context.Context, roomID, sessionID string) error {
	return service.prepareResync(ctx, roomID, sessionID, ResyncReconnect)
}

func (service *Service) PrepareHashResync(ctx context.Context, roomID, sessionID string) error {
	return service.prepareResync(ctx, roomID, sessionID, ResyncHash)
}

func (service *Service) PrepareHostResync(ctx context.Context, roomID, sessionID string) error {
	return service.prepareResync(ctx, roomID, sessionID, ResyncHost)
}

func controlIdentity(peer SocketParticipant) PeerIdentity {
	return PeerIdentity{
		RoomID:               peer.RoomID,
		SessionID:            peer.SessionID,
		ProfileID:            peer.ProfileID,
		PlayerNo:             peer.PlayerNo,
		CredentialGeneration: peer.CredentialGeneration,
	}
}

func (service *Service) MarkDisconnected(ctx context.Context, peer SocketParticipant) error {
	if err := service.components.Sessions.Disconnected(ctx, controlIdentity(peer)); err != nil {
		return applicationError("mark disconnected", err)
	}
	return nil
}

func (service *Service) MarkRuntimeReady(ctx context.Context, peer SocketParticipant) (bool, error) {
	all, err := service.components.Sessions.RuntimeReady(ctx, controlIdentity(peer))
	if err != nil {
		return false, applicationError("runtime ready", err)
	}
	return all, nil
}

func (service *Service) MarkSessionRunning(ctx context.Context, roomID, sessionID string) error {
	if err := service.components.Sessions.Running(ctx, roomID, sessionID); err != nil {
		return applicationError("session running", err)
	}
	return nil
}
