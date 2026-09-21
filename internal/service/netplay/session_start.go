package netplay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"retrom/internal/netplay/profile"
	validation "retrom/internal/service/corevalidation"
)

type SessionStartPlan struct {
	Before    RoomControlSnapshot
	SessionID string
	SessionNo int
	Profile   FrozenRoomProfile
	Members   []SeatMember
	SeatMask  int
	Now       int64
	Event     []byte
}
type SessionStartWriter interface {
	NextNumber(context.Context, string) (int, error)
	Insert(context.Context, SessionStartPlan) (Room, error)
}
type SessionStartScope struct {
	Read        RoomControlReader
	Write       SessionStartWriter
	Eligibility EligibilityRepository
	BIOS        validation.Repository
}
type SessionStartRepository interface {
	WithStart(context.Context, func(SessionStartScope) error) error
}
type SessionStart struct {
	repository SessionStartRepository
	registry   *profile.Registry
	now        func() time.Time
	newID      func() (string, error)
}

func NewSessionStart(
	repository SessionStartRepository,
	registry *profile.Registry,
	now func() time.Time,
) *SessionStart {
	return &SessionStart{repository: repository, registry: registry, now: now, newID: roomUUID}
}

func (service *SessionStart) Start(ctx context.Context, roomID, hostID string, version int64) (Room, error) {
	now := service.now().UnixMilli()
	var result Room
	err := service.repository.WithStart(ctx, func(scope SessionStartScope) error {
		before, err := scope.Read.Current(ctx, roomID, hostID)
		if err != nil {
			return fmt.Errorf("netplay/start room snapshot: %w", err)
		}
		if before.HostID != hostID {
			return ErrForbidden
		}
		if before.Version != version {
			return ErrPrecondition
		}
		if before.State != RoomStateWaiting {
			return ErrRoomConflict
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
		return Room{}, fmt.Errorf("netplay/start: %w", err)
	}
	return roomForViewer(result, hostID, now), nil
}

func (service *SessionStart) lockedProfile(
	ctx context.Context,
	scope SessionStartScope,
	selected *RoomSelection,
) (FrozenRoomProfile, error) {
	if selected == nil {
		return FrozenRoomProfile{}, ErrProfileStale
	}
	eligibility := NewEligibility(scope.Eligibility, service.registry, nil, validation.New(scope.BIOS))
	candidates, err := eligibility.Profiles(ctx, selected.GameID)
	if err != nil {
		return FrozenRoomProfile{}, fmt.Errorf("netplay/start eligibility: %w", err)
	}
	for _, candidate := range candidates {
		if candidate.Manifest.ID != selected.ProfileID || candidate.VariantID != selected.VariantID {
			continue
		}
		frozen, err := freezeRoomProfile(service.registry, selected.GameID, candidate)
		if errors.Is(err, ErrInvalidProfile) {
			return FrozenRoomProfile{}, ErrProfileStale
		}
		if err != nil {
			return FrozenRoomProfile{}, err
		}
		if frozen.Selection != *selected {
			return FrozenRoomProfile{}, ErrProfileStale
		}
		return frozen, nil
	}
	return FrozenRoomProfile{}, ErrProfileStale
}

func startSeatMask(before RoomControlSnapshot) (int, error) {
	if before.Selection == nil || len(before.Occupants) < 2 || len(before.Occupants) > before.Selection.MaxPlayers {
		return 0, ErrRoomNotReady
	}
	mask := 0
	for _, member := range before.Occupants {
		if !member.Ready || member.LeftAtMS != nil || member.PlayerNo < 1 || member.PlayerNo > before.Selection.MaxPlayers {
			return 0, ErrRoomNotReady
		}
		bit := 1 << (member.PlayerNo - 1)
		if mask&bit != 0 {
			return 0, ErrRoomNotReady
		}
		if member.PlayerNo == 1 && (member.Role != "HOST" || member.ProfileID != before.HostID) {
			return 0, ErrRoomNotReady
		}
		mask |= bit
	}
	if mask&1 == 0 {
		return 0, ErrRoomNotReady
	}
	return mask, nil
}

func (service *SessionStart) plan(
	ctx context.Context,
	scope SessionStartScope,
	before RoomControlSnapshot,
	frozen FrozenRoomProfile,
	mask int,
	now int64,
) (SessionStartPlan, error) {
	sessionNo, err := scope.Write.NextNumber(ctx, before.RoomID)
	if err != nil {
		return SessionStartPlan{}, fmt.Errorf("netplay/session number: %w", err)
	}
	id, err := service.newID()
	if err != nil {
		return SessionStartPlan{}, fmt.Errorf("netplay/session identity: %w", err)
	}
	data, err := json.Marshal(struct {
		SchemaVersion    int `json:"schemaVersion"`
		PlayerCount      int `json:"playerCount"`
		OccupiedSeatMask int `json:"occupiedSeatMask"`
	}{1, len(before.Occupants), mask})
	if err != nil {
		return SessionStartPlan{}, fmt.Errorf("netplay/start event: %w", err)
	}
	return SessionStartPlan{
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
