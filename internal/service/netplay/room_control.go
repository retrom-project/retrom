package netplay

import (
	"context"
	"fmt"
	"slices"
	"time"

	model "retrom/internal/model/netplay"
	"retrom/internal/model/netplayprofile"
)

type RoomControl struct {
	repository             model.RoomControlRepository
	registry               *netplayprofile.Registry
	draftIdle, waitingIdle time.Duration
	now                    func() time.Time
	newID                  func() (string, error)
}

func NewRoomControl(
	repository model.RoomControlRepository,
	registry *netplayprofile.Registry,
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
	apply func(model.RoomControlScope, model.RoomControlSnapshot, int64) error,
) (model.Room, error) {
	now := service.now().UnixMilli()
	var result model.Room
	err := service.repository.WithWrite(ctx, func(scope model.RoomControlScope) error {
		before, err := scope.Read.Current(ctx, request.roomID, request.actorID)
		if err != nil {
			return fmt.Errorf("netplay/read room control: %w", err)
		}
		if request.hostOnly && before.HostID != request.actorID {
			return model.ErrForbidden
		}
		if before.Version != request.version {
			return model.ErrPrecondition
		}
		if !slices.Contains(request.states, before.State) {
			return model.ErrRoomConflict
		}
		if err := apply(scope, before, now); err != nil {
			return err
		}
		result, err = scope.Read.Snapshot(ctx, request.roomID)
		if err != nil {
			return fmt.Errorf("netplay/read updated room: %w", err)
		}
		return nil
	})
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
	eligibility := NewEligibility(scope.Eligibility, service.registry, nil, scope.BIOS)
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
