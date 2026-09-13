package netplay

import (
	"context"
	"fmt"
	"slices"
	"time"

	validation "retrom/internal/service/corevalidation"
	"retrom/internal/transport/netplay/profile"
)

type RoomControl struct {
	repository             RoomControlRepository
	registry               *profile.Registry
	draftIdle, waitingIdle time.Duration
	now                    func() time.Time
	newID                  func() (string, error)
}

func NewRoomControl(
	repository RoomControlRepository,
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
	apply func(RoomControlScope, RoomControlSnapshot, int64) error,
) (Room, error) {
	now := service.now().UnixMilli()
	var result Room
	err := service.repository.WithWrite(ctx, func(scope RoomControlScope) error {
		before, err := scope.Read.Current(ctx, request.roomID, request.actorID)
		if err != nil {
			return fmt.Errorf("netplay/read room control: %w", err)
		}
		if request.hostOnly && before.HostID != request.actorID {
			return ErrForbidden
		}
		if before.Version != request.version {
			return ErrPrecondition
		}
		if !slices.Contains(request.states, before.State) {
			return ErrRoomConflict
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
		return Room{}, fmt.Errorf("netplay/mutate room: %w", err)
	}
	return roomForViewer(result, request.actorID, now), nil
}

func (service *RoomControl) eligible(
	ctx context.Context,
	scope RoomControlScope,
	gameID string,
) ([]EligibleProfile, error) {
	eligibility := NewEligibility(scope.Eligibility, service.registry, nil, validation.New(scope.BIOS))
	profiles, err := eligibility.Profiles(ctx, gameID)
	if err != nil {
		return nil, fmt.Errorf("netplay/room eligibility: %w", err)
	}
	return profiles, nil
}

func (service *RoomControl) selection(
	ctx context.Context,
	scope RoomControlScope,
	gameID, profileID string,
) (RoomSelection, error) {
	profiles, err := service.eligible(ctx, scope, gameID)
	if err != nil {
		return RoomSelection{}, err
	}
	for _, candidate := range profiles {
		if candidate.Manifest.ID != profileID {
			continue
		}
		frozen, err := freezeRoomProfile(service.registry, gameID, candidate)
		if err != nil {
			return RoomSelection{}, err
		}
		return frozen.Selection, nil
	}
	return RoomSelection{}, ErrInvalidProfile
}
