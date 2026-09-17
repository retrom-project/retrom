package netplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	model "retrom/internal/model/netplay"

	"retrom/internal/transport/netplay/profile"
)

type RoomControl struct {
	repository             model.RoomControlRepository
	eligibility            model.EligibilityRepository
	bios                   model.BIOSResolver
	registry               *profile.Registry
	draftIdle, waitingIdle time.Duration
	now                    func() time.Time
	newID                  func() (string, error)
}

func NewRoomControl(
	repository model.RoomControlRepository,
	eligibility model.EligibilityRepository,
	bios model.BIOSResolver,
	registry *profile.Registry,
	draftIdle, waitingIdle time.Duration,
	now func() time.Time,
) *RoomControl {
	return &RoomControl{
		repository:  repository,
		eligibility: eligibility,
		bios:        bios,
		registry:    registry,
		draftIdle:   draftIdle,
		waitingIdle: waitingIdle,
		now:         now,
		newID:       roomUUID,
	}
}

func (service *RoomControl) eligible(
	ctx context.Context,
	gameID string,
) ([]model.EligibleProfile, error) {
	eligibility := NewEligibility(
		service.eligibility, service.registry, nil, service.bios,
	)
	profiles, err := eligibility.Profiles(ctx, gameID)
	if err != nil {
		return nil, fmt.Errorf("netplay/room eligibility: %w", err)
	}
	return profiles, nil
}

func (service *RoomControl) selection(
	ctx context.Context,
	gameID, profileID string,
) (model.RoomSelection, error) {
	profiles, err := service.eligible(ctx, gameID)
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

func (service *RoomControl) requireCurrentSelection(
	ctx context.Context,
	locked *model.RoomSelection,
) error {
	if locked == nil {
		return model.ErrProfileStale
	}
	current, err := service.selection(ctx, locked.GameID, locked.ProfileID)
	if errors.Is(err, model.ErrInvalidProfile) {
		return model.ErrProfileStale
	}
	if err != nil {
		return err
	}
	if current != *locked {
		return model.ErrProfileStale
	}
	return nil
}
