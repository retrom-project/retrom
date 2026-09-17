package netplay

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/netplay"

	validation "retrom/internal/service/corevalidation"
	"retrom/internal/transport/netplay/profile"
)

type RoomControl struct {
	repository             model.RoomControlRepository
	registry               *profile.Registry
	draftIdle, waitingIdle time.Duration
	now                    func() time.Time
	newID                  func() (string, error)
}

func NewRoomControl(
	repository model.RoomControlRepository,
	registry *profile.Registry,
	draftIdle, waitingIdle time.Duration,
	now func() time.Time,
) *RoomControl {
	return &RoomControl{
		repository:  repository,
		registry:    registry,
		draftIdle:   draftIdle,
		waitingIdle: waitingIdle,
		now:         now,
		newID:       roomUUID,
	}
}

type roomMutation struct {
	roomID, actorID string
	version         int64
	hostOnly        bool
	states          []string
}

func (service *RoomControl) mutate(
	ctx context.Context,
	request roomMutation,
	apply model.MutationFunc,
) (model.Room, error) {
	now := service.now().UnixMilli()
	result, err := service.repository.CommitMutation(ctx, model.MutationCommand{
		RoomID:   request.roomID,
		ActorID:  request.actorID,
		Version:  request.version,
		HostOnly: request.hostOnly,
		States:   request.states,
		NowMS:    now,
	}, apply)
	if err != nil {
		return model.Room{}, fmt.Errorf("netplay/mutate room: %w", err)
	}
	return roomForViewer(result, request.actorID, now), nil
}

func (service *RoomControl) eligible(
	ctx context.Context,
	scope model.RoomControlScope,
	gameID string,
) ([]model.EligibleProfile, error) {
	eligibility := NewEligibility(scope.Eligibility, service.registry, nil, validation.New(scope.BIOS))
	profiles, err := eligibility.Profiles(ctx, gameID)
	if err != nil {
		return nil, fmt.Errorf("netplay/room eligibility: %w", err)
	}
	return profiles, nil
}

func (service *RoomControl) selection(
	ctx context.Context,
	scope model.RoomControlScope,
	gameID, profileID string,
) (model.RoomSelection, error) {
	profiles, err := service.eligible(ctx, scope, gameID)
	if err != nil {
		return model.RoomSelection{}, err
	}
	for _, candidate := range profiles {
		if candidate.Manifest.ID != profileID {
			continue
		}
		frozen, err := freezeRoomProfile(service.registry, gameID, candidate)
		if err != nil {
			return model.RoomSelection{}, err
		}
		return frozen.Selection, nil
	}
	return model.RoomSelection{}, model.ErrInvalidProfile
}
