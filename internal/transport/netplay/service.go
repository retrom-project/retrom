package netplay

import (
	"fmt"

	netplaymodel "retrom/internal/model/netplay"
	netplayservice "retrom/internal/service/netplay"
)

const (
	RoomStateDraft    = "DRAFT"
	RoomStateWaiting  = "WAITING"
	RoomStateStarting = "STARTING"
	RoomStateRunning  = "RUNNING"
)

var (
	ErrRoomNotFound    = netplaymodel.ErrRoomNotFound
	ErrSessionNotFound = netplaymodel.ErrSessionNotFound
	ErrForbidden       = netplaymodel.ErrForbidden
	ErrInvalidSeat     = netplaymodel.ErrInvalidSeat
	ErrInvalidProfile  = netplaymodel.ErrInvalidProfile
	ErrSeatTaken       = netplaymodel.ErrSeatTaken
	ErrRoomNotReady    = netplaymodel.ErrRoomNotReady
	ErrRoomConflict    = netplaymodel.ErrRoomConflict
	ErrProfileStale    = netplaymodel.ErrProfileStale
	ErrCapacity        = netplaymodel.ErrCapacity
	ErrPrecondition    = netplaymodel.ErrPrecondition
)

func serviceError(operation string, err error) error {
	return fmt.Errorf("netplay/%s: %w", operation, err)
}

const (
	RoomDispositionWaiting = "WAITING"
	RoomDispositionEnded   = "ENDED"
)

func endDisposition(reason string, actorIsHost bool) string {
	return netplayservice.EndDisposition(reason, actorIsHost)
}

type resyncCause string

const (
	resyncReconnect resyncCause = "PEER_RECONNECTED"
	resyncHash      resyncCause = "STATE_MISMATCH"
	resyncHost      resyncCause = "HOST_RESUME"
)

type Options = netplayservice.Options
