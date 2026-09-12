package netplay

import (
	"context"

	"retrom/internal/launch"
	application "retrom/internal/service/netplay"
)

type (
	Event             = application.Event
	ParticipantLaunch = application.ParticipantLaunchResult
	SocketParticipant = application.SocketParticipant
)

func (service *Service) CreateParticipantLaunch(
	ctx context.Context,
	launcher *launch.Service,
	roomID, sessionID, profileID string,
	capabilities launch.Capabilities,
) (ParticipantLaunch, error) {
	result, err := service.preparation.Launch(
		ctx,
		launcher,
		application.PreparationRequest{
			RoomID:       roomID,
			SessionID:    sessionID,
			ProfileID:    profileID,
			Capabilities: capabilities,
		},
	)
	if err != nil {
		return ParticipantLaunch{}, serviceError("prepare participant", err)
	}
	return result, nil
}
