package netplay

import "errors"

const (
	RoomStateDraft    = "DRAFT"
	RoomStateWaiting  = "WAITING"
	RoomStateStarting = "STARTING"
	RoomStateRunning  = "RUNNING"
)

var (
	ErrRoomNotFound          = errors.New("NETPLAY_ROOM_NOT_FOUND")
	ErrSessionNotFound       = errors.New("NETPLAY_SESSION_NOT_FOUND")
	ErrForbidden             = errors.New("NETPLAY_FORBIDDEN")
	ErrInvalidSeat           = errors.New("NETPLAY_INVALID_SEAT")
	ErrSeatTaken             = errors.New("NETPLAY_SEAT_TAKEN")
	ErrRoomNotReady          = errors.New("NETPLAY_ROOM_NOT_READY")
	ErrRoomConflict          = errors.New("NETPLAY_ROOM_STATE_CONFLICT")
	ErrProfileStale          = errors.New("NETPLAY_PROFILE_STALE")
	ErrCapacity              = errors.New("NETPLAY_CAPACITY_REACHED")
	ErrPrecondition          = errors.New("PRECONDITION_FAILED")
	ErrInvalidRecoveryReason = errors.New("netplay/recovery: invalid reason")
)
