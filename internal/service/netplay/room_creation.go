package netplay

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type RoomCapacity struct {
	Active     int
	HostActive bool
}
type RoomCreationPlan struct {
	RoomID, MemberID, HostID string
	Now, ExpiresAtMS         int64
	Event                    []byte
}
type RoomCreationWriter interface {
	Capacity(context.Context, string) (RoomCapacity, error)
	Insert(context.Context, RoomCreationPlan) (Room, error)
}
type RoomCreationRepository interface {
	WithCreate(context.Context, func(RoomCreationWriter) error) error
}
type RoomCreation struct {
	repository RoomCreationRepository
	maximum    int
	idle       time.Duration
	now        func() time.Time
	newID      func() (string, error)
}

func NewRoomCreation(
	repository RoomCreationRepository, maximum int, idle time.Duration, now func() time.Time,
) *RoomCreation {
	return &RoomCreation{repository: repository, maximum: maximum, idle: idle, now: now, newID: roomUUID}
}

func roomUUID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("netplay/new room identity: %w", err)
	}
	return id.String(), nil
}

func (service *RoomCreation) Create(ctx context.Context, hostID string) (Room, error) {
	now := service.now().UnixMilli()
	var result Room
	err := service.repository.WithCreate(ctx, func(writer RoomCreationWriter) error {
		capacity, err := writer.Capacity(ctx, hostID)
		if err != nil {
			return fmt.Errorf("netplay/room capacity: %w", err)
		}
		if capacity.HostActive {
			return ErrRoomConflict
		}
		if capacity.Active >= service.maximum {
			return ErrCapacity
		}
		plan, err := service.plan(hostID, now)
		if err != nil {
			return err
		}
		result, err = writer.Insert(ctx, plan)
		if err != nil {
			return fmt.Errorf("netplay/persist room: %w", err)
		}
		return nil
	})
	if err != nil {
		return Room{}, fmt.Errorf("netplay/create room: %w", err)
	}
	return roomForViewer(result, hostID, now), nil
}

func (service *RoomCreation) plan(hostID string, now int64) (RoomCreationPlan, error) {
	roomID, err := service.newID()
	if err != nil {
		return RoomCreationPlan{}, fmt.Errorf("netplay/room identity: %w", err)
	}
	memberID, err := service.newID()
	if err != nil {
		return RoomCreationPlan{}, fmt.Errorf("netplay/member identity: %w", err)
	}
	return RoomCreationPlan{
		RoomID: roomID, MemberID: memberID, HostID: hostID, Now: now,
		ExpiresAtMS: now + service.idle.Milliseconds(), Event: []byte(`{"schemaVersion":1}`),
	}, nil
}
