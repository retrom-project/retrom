package netplay

import (
	"context"

	model "retrom/internal/model/netplay"

	launch "retrom/internal/model/launch"
)

func (service *Service) Games(ctx context.Context, profileID, availability string) ([]model.GameSummary, error) {
	items, err := service.components.Eligibility.Games(ctx, profileID, availability)
	if err != nil {
		return nil, applicationError("games", err)
	}
	return items, nil
}

func (service *Service) GamePage(
	ctx context.Context, profileID, availability, title, gameID string, limit int,
) ([]model.GameSummary, bool, error) {
	items, more, err := service.components.Eligibility.GamePage(ctx, profileID, availability, title, gameID, limit)
	if err != nil {
		return nil, false, applicationError("game page", err)
	}
	return items, more, nil
}

func (service *Service) AuthenticateSocket(
	ctx context.Context,
	roomID, profileID, encoded string,
) (model.SocketParticipant, error) {
	peer, err := service.components.Access.Authenticate(ctx, roomID, profileID, encoded)
	if err != nil {
		return model.SocketParticipant{}, applicationError("authenticate socket", err)
	}
	return peer, nil
}

func (service *Service) ParticipantCapability(ctx context.Context, sessionID, profileID string) (string, error) {
	value, err := service.components.Access.Capability(ctx, sessionID, profileID)
	if err != nil {
		return "", applicationError("participant capability", err)
	}
	return value, nil
}

func (service *Service) CreateParticipantLaunch(
	ctx context.Context,
	launcher model.NetplayLauncher,
	roomID, sessionID, profileID string,
	capabilities launch.Capabilities,
) (model.ParticipantLaunchResult, error) {
	result, err := service.components.Preparation.Launch(
		ctx,
		launcher,
		model.PreparationRequest{
			RoomID:       roomID,
			SessionID:    sessionID,
			ProfileID:    profileID,
			Capabilities: capabilities,
		},
	)
	if err != nil {
		return model.ParticipantLaunchResult{}, applicationError("prepare participant", err)
	}
	return result, nil
}

func (service *Service) SupportsPlatformTarget(
	platformID, coreID, providerID, targetID string,
) bool {
	return service != nil && service.registry.SupportsPlatformTarget(
		platformID, coreID, providerID, targetID,
	)
}
