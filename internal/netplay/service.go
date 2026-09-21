package netplay

import (
	"fmt"

	application "retrom/internal/service/netplay"
)

const (
	RoomStateDraft    = "DRAFT"
	RoomStateWaiting  = "WAITING"
	RoomStateStarting = "STARTING"
	RoomStateRunning  = "RUNNING"
)

var (
	ErrRoomNotFound    = application.ErrRoomNotFound
	ErrSessionNotFound = application.ErrSessionNotFound
	ErrForbidden       = application.ErrForbidden
	ErrInvalidSeat     = application.ErrInvalidSeat
	ErrInvalidProfile  = application.ErrInvalidProfile
	ErrSeatTaken       = application.ErrSeatTaken
	ErrRoomNotReady    = application.ErrRoomNotReady
	ErrRoomConflict    = application.ErrRoomConflict
	ErrProfileStale    = application.ErrProfileStale
	ErrCapacity        = application.ErrCapacity
	ErrPrecondition    = application.ErrPrecondition
)

func serviceError(operation string, err error) error {
	return fmt.Errorf("netplay/%s: %w", operation, err)
}

const (
	RoomDispositionWaiting = "WAITING"
	RoomDispositionEnded   = "ENDED"
)

func endDisposition(reason string, actorIsHost bool) string {
	return application.EndDisposition(reason, actorIsHost)
}

type resyncCause string

const (
	resyncReconnect resyncCause = "PEER_RECONNECTED"
	resyncHash      resyncCause = "STATE_MISMATCH"
	resyncHost      resyncCause = "HOST_RESUME"
)

type Options = application.Options
