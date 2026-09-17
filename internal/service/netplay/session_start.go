package netplay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	model "retrom/internal/model/netplay"

	validation "retrom/internal/service/corevalidation"
	"retrom/internal/transport/netplay/profile"
)

type SessionStart struct {
	repository model.SessionStartRepository
	registry   *profile.Registry
	now        func() time.Time
	newID      func() (string, error)
}

func NewSessionStart(
	repository model.SessionStartRepository,
	registry *profile.Registry,
	now func() time.Time,
) *SessionStart {
	return &SessionStart{repository: repository, registry: registry, now: now, newID: roomUUID}
}

func (service *SessionStart) Start(ctx context.Context, roomID, hostID string, version int64) (model.Room, error) {
	now := service.now().UnixMilli()
	var result model.Room
	err := service.repository.WithStart(ctx, func(scope model.SessionStartScope) error {
		before, err := scope.Read.Current(ctx, roomID, hostID)
		if err != nil {
			return fmt.Errorf("netplay/start room snapshot: %w", err)
		}
		if before.HostID != hostID {
			return model.ErrForbidden
		}
		if before.Version != version {
			return model.ErrPrecondition
		}
		if before.State != model.RoomStateWaiting {
			return model.ErrRoomConflict
		}
		frozen, err := service.lockedProfile(ctx, scope, before.Selection)
		if err != nil {
			return err
		}
		mask, err := startSeatMask(before)
		if err != nil {
			return err
		}
		plan, err := service.plan(ctx, scope, before, frozen, mask, now)
		if err != nil {
			return err
		}
		result, err = scope.Write.Insert(ctx, plan)
		if err != nil {
			return fmt.Errorf("netplay/start session: %w", err)
		}
		return nil
	})
	if err != nil {
		return model.Room{}, fmt.Errorf("netplay/start: %w", err)
	}
	return roomForViewer(result, hostID, now), nil
}

func (service *SessionStart) lockedProfile(
	ctx context.Context,
	scope model.SessionStartScope,
	selected *model.RoomSelection,
) (model.FrozenRoomProfile, error) {
	if selected == nil {
		return model.FrozenRoomProfile{}, model.ErrProfileStale
	}
	eligibility := NewEligibility(scope.Eligibility, service.registry, nil, validation.New(scope.BIOS))
	candidates, err := eligibility.Profiles(ctx, selected.GameID)
	if err != nil {
		return model.FrozenRoomProfile{}, fmt.Errorf("netplay/start eligibility: %w", err)
	}
	for _, candidate := range candidates {
		if candidate.Manifest.ID != selected.ProfileID || candidate.VariantID != selected.VariantID {
			continue
		}
		frozen, err := freezeRoomProfile(service.registry, selected.GameID, candidate)
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

func (service *SessionStart) plan(
	ctx context.Context,
	scope model.SessionStartScope,
	before model.RoomControlSnapshot,
	frozen model.FrozenRoomProfile,
	mask int,
	now int64,
) (model.SessionStartPlan, error) {
	sessionNo, err := scope.Write.NextNumber(ctx, before.RoomID)
	if err != nil {
		return model.SessionStartPlan{}, fmt.Errorf("netplay/session number: %w", err)
	}
	id, err := service.newID()
	if err != nil {
		return model.SessionStartPlan{}, fmt.Errorf("netplay/session identity: %w", err)
	}
	data, err := json.Marshal(struct {
		SchemaVersion    int `json:"schemaVersion"`
		PlayerCount      int `json:"playerCount"`
		OccupiedSeatMask int `json:"occupiedSeatMask"`
	}{1, len(before.Occupants), mask})
	if err != nil {
		return model.SessionStartPlan{}, fmt.Errorf("netplay/start event: %w", err)
	}
	return model.SessionStartPlan{
		Before:    before,
		SessionID: id,
		SessionNo: sessionNo,
		Profile:   frozen,
		Members:   before.Occupants,
		SeatMask:  mask,
		Now:       now,
		Event:     data,
	}, nil
}
