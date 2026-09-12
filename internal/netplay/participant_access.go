package netplay

import (
	"context"

	repository "retrom/internal/persistence/netplay"
	application "retrom/internal/service/netplay"
)

func (service *Service) participantAccess() *application.ParticipantAccess {
	return application.NewParticipantAccess(repository.NewParticipantAccess(service.database), service.credentials)
}

func (service *Service) AuthenticateSocket(
	ctx context.Context,
	roomID, profileID, encoded string,
) (SocketParticipant, error) {
	peer, err := service.participantAccess().Authenticate(ctx, roomID, profileID, encoded)
	if err != nil {
		return SocketParticipant{}, serviceError("authenticate socket", err)
	}
	return peer, nil
}

func (service *Service) ParticipantCapability(ctx context.Context, sessionID, profileID string) (string, error) {
	value, err := service.participantAccess().Capability(ctx, sessionID, profileID)
	if err != nil {
		return "", serviceError("participant capability", err)
	}
	return value, nil
}
