package netplay

import (
	"context"

	repository "retrom/internal/persistence/netplay"
	application "retrom/internal/service/netplay"
)

func (service *Service) sessionControl() *application.SessionControl {
	return application.NewSessionControl(
		repository.NewSessionControl(service.database),
		service.options.ReconnectLease,
		service.clock.Now,
	)
}

func (service *Service) SetSessionState(ctx context.Context, roomID, sessionID, actorID, target string) error {
	if err := service.sessionControl().SetState(ctx, roomID, sessionID, actorID, target); err != nil {
		return serviceError("set session state", err)
	}
	return nil
}

func (service *Service) prepareResync(ctx context.Context, roomID, sessionID string, cause resyncCause) error {
	if err := service.sessionControl().Resync(ctx, roomID, sessionID, application.ResyncCause(cause)); err != nil {
		return serviceError("prepare resync", err)
	}
	return nil
}

func validResyncSource(cause resyncCause, state string) bool {
	return application.ValidResyncSource(application.ResyncCause(cause), state)
}

func (service *Service) PrepareReconnectResync(ctx context.Context, roomID, sessionID string) error {
	return service.prepareResync(ctx, roomID, sessionID, resyncReconnect)
}

func (service *Service) PrepareHashResync(ctx context.Context, roomID, sessionID string) error {
	return service.prepareResync(ctx, roomID, sessionID, resyncHash)
}

func (service *Service) PrepareHostResync(ctx context.Context, roomID, sessionID string) error {
	return service.prepareResync(ctx, roomID, sessionID, resyncHost)
}

func controlIdentity(peer SocketParticipant) application.PeerIdentity {
	return application.PeerIdentity{
		RoomID:               peer.RoomID,
		SessionID:            peer.SessionID,
		ProfileID:            peer.ProfileID,
		PlayerNo:             peer.PlayerNo,
		CredentialGeneration: peer.CredentialGeneration,
	}
}

func (service *Service) MarkDisconnected(ctx context.Context, peer SocketParticipant) error {
	if err := service.sessionControl().Disconnected(ctx, controlIdentity(peer)); err != nil {
		return serviceError("mark disconnected", err)
	}
	return nil
}

func (service *Service) MarkRuntimeReady(ctx context.Context, peer SocketParticipant) (bool, error) {
	all, err := service.sessionControl().RuntimeReady(ctx, controlIdentity(peer))
	if err != nil {
		return false, serviceError("runtime ready", err)
	}
	return all, nil
}

func (service *Service) MarkSessionRunning(ctx context.Context, roomID, sessionID string) error {
	if err := service.sessionControl().Running(ctx, roomID, sessionID); err != nil {
		return serviceError("session running", err)
	}
	return nil
}
