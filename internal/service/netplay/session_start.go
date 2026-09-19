package netplay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	model "retrom/internal/model/netplay"

	"retrom/internal/transport/netplay/profile"
)

type SessionStart struct {
	repository  model.SessionStartRepository
	eligibility model.EligibilityRepository
	bios        model.BIOSResolver
	registry    *profile.Registry
	now         func() time.Time
	newID       func() (string, error)
}

func NewSessionStart(
	repository model.SessionStartRepository,
	eligibility model.EligibilityRepository,
	bios model.BIOSResolver,
	registry *profile.Registry,
	now func() time.Time,
) *SessionStart {
	return &SessionStart{repository: repository, eligibility: eligibility, bios: bios, registry: registry, now: now, newID: roomUUID}
}

func (service *SessionStart) Start(ctx context.Context, roomID, hostID string, version int64) (model.Room, error) {
	now := service.now().UnixMilli()
	before, err := service.preReadRoom(ctx, roomID, hostID, version)
	if err != nil {
		return model.Room{}, fmt.Errorf("netplay/start: %w", err)
	}
	frozen, err := service.lockedProfilePreRead(ctx, before.Selection)
	if err != nil {
		return model.Room{}, fmt.Errorf("netplay/start: %w", err)
	}
	mask, err := startSeatMask(before)
	if err != nil {
		return model.Room{}, fmt.Errorf("netplay/start: %w", err)
	}
	id, err := service.newID()
	if err != nil {
		return model.Room{}, fmt.Errorf("netplay/session identity: %w", err)
	}
	data, err := json.Marshal(struct {
		SchemaVersion    int `json:"schemaVersion"`
		PlayerCount      int `json:"playerCount"`
		OccupiedSeatMask int `json:"occupiedSeatMask"`
	}{1, len(before.Occupants), mask})
	if err != nil {
		return model.Room{}, fmt.Errorf("netplay/start event: %w", err)
	}
	cmd := model.SessionStartCommand{
		RoomID:          roomID,
		HostID:          hostID,
		ExpectedVersion: version,
		FrozenProfile:   frozen,
		SessionID:       id,
		NowMS:           now,
		SeatMask:        mask,
		Members:         before.Occupants,
		Event:           data,
	}
	result, err := service.repository.CommitSessionStart(ctx, cmd)
	if err != nil {
		return model.Room{}, fmt.Errorf("netplay/start: %w", err)
	}
	return roomForViewer(result, hostID, now), nil
}

func (service *SessionStart) preReadRoom(ctx context.Context, roomID, hostID string, version int64) (model.RoomControlSnapshot, error) {
	before, err := service.repository.InspectRoom(ctx, roomID, hostID)
	if err != nil {
		return model.RoomControlSnapshot{}, fmt.Errorf("netplay/start room snapshot: %w", err)
	}
	if before.HostID != hostID {
		return model.RoomControlSnapshot{}, model.ErrForbidden
	}
	if before.Version != version {
		return model.RoomControlSnapshot{}, model.ErrPrecondition
	}
	if before.State != model.RoomStateWaiting {
		return model.RoomControlSnapshot{}, model.ErrRoomConflict
	}
	return before, nil
}

func (service *SessionStart) lockedProfilePreRead(
	ctx context.Context,
	selected *model.RoomSelection,
) (model.FrozenRoomProfile, error) {
	if selected == nil {
		return model.FrozenRoomProfile{}, model.ErrProfileStale
	}
	eligibility := NewEligibility(service.eligibility, service.registry, nil, service.bios)
	candidates, err := eligibility.Profiles(ctx, selected.GameID)
	if err != nil {
		return model.FrozenRoomProfile{}, fmt.Errorf("netplay/start eligibility: %w", err)
	}
	return matchRoomProfile(service.registry, selected, candidates)
}

func matchRoomProfile(
	registry *profile.Registry,
	selected *model.RoomSelection,
	candidates []model.EligibleProfile,
) (model.FrozenRoomProfile, error) {
	for _, candidate := range candidates {
		if candidate.Manifest.ID != selected.ProfileID || candidate.VariantID != selected.VariantID {
			continue
		}
		frozen, err := freezeRoomProfile(registry, selected.GameID, candidate)
		if errors.Is(err, model.ErrInvalidProfile) {
			return model.FrozenRoomProfile{}, model.ErrProfileStale
		}
		if err != nil {
			return model.FrozenRoomProfile{}, err
		}
		if frozen.Selection != *selected {
			return model.FrozenRoomProfile{}, model.ErrProfileStale
		}
		return frozen, nil
	}
	return model.FrozenRoomProfile{}, model.ErrProfileStale
}

func startSeatMask(before model.RoomControlSnapshot) (int, error) {
	if before.Selection == nil || len(before.Occupants) < 2 || len(before.Occupants) > before.Selection.MaxPlayers {
		return 0, model.ErrRoomNotReady
	}
	mask := 0
	for _, member := range before.Occupants {
		if !member.Ready || member.LeftAtMS != nil || member.PlayerNo < 1 || member.PlayerNo > before.Selection.MaxPlayers {
			return 0, model.ErrRoomNotReady
		}
		bit := 1 << (member.PlayerNo - 1)
		if mask&bit != 0 {
			return 0, model.ErrRoomNotReady
		}
		if member.PlayerNo == 1 && (member.Role != "HOST" || member.ProfileID != before.HostID) {
			return 0, model.ErrRoomNotReady
		}
		mask |= bit
	}
	if mask&1 == 0 {
		return 0, model.ErrRoomNotReady
	}
	return mask, nil
}

