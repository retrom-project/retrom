package netplay

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/netplay"

	"github.com/google/uuid"
)

type RoomCreation struct {
	repository model.RoomCreationRepository
	maximum    int
	idle       time.Duration
	now        func() time.Time
	newID      func() (string, error)
}

func NewRoomCreation(
	repository model.RoomCreationRepository, maximum int, idle time.Duration, now func() time.Time,
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

func (service *RoomCreation) Create(ctx context.Context, hostID string) (model.Room, error) {
	now := service.now().UnixMilli()
	plan, err := service.plan(hostID, now)
	if err != nil {
		return model.Room{}, fmt.Errorf("netplay/create room: %w", err)
	}
	result, err := service.repository.CommitRoomCreation(
		ctx, model.RoomCreationCommand{Plan: plan, Maximum: service.maximum},
	)
	if err != nil {
		return model.Room{}, fmt.Errorf("netplay/create room: %w", err)
	}
	return roomForViewer(result, hostID, now), nil
}

func (service *RoomCreation) plan(hostID string, now int64) (model.RoomCreationPlan, error) {
	roomID, err := service.newID()
	if err != nil {
		return model.RoomCreationPlan{}, fmt.Errorf("netplay/room identity: %w", err)
	}
	memberID, err := service.newID()
	if err != nil {
		return model.RoomCreationPlan{}, fmt.Errorf("netplay/member identity: %w", err)
	}
	return model.RoomCreationPlan{
		RoomID: roomID, MemberID: memberID, HostID: hostID, Now: now,
		ExpiresAtMS: now + service.idle.Milliseconds(), Event: []byte(`{"schemaVersion":1}`),
	}, nil
}
