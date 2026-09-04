package netplay

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"retrom/internal/tagging"
)

const (
	RoomStateDraft    = "DRAFT"
	RoomStateWaiting  = "WAITING"
	RoomStateStarting = "STARTING"
	RoomStateRunning  = "RUNNING"
)

var (
	ErrRoomNotFound    = errors.New("NETPLAY_ROOM_NOT_FOUND")
	ErrSessionNotFound = errors.New("NETPLAY_SESSION_NOT_FOUND")
	ErrForbidden       = errors.New("NETPLAY_FORBIDDEN")
	ErrInvalidSeat     = errors.New("NETPLAY_INVALID_SEAT")
	ErrInvalidProfile  = errors.New("NETPLAY_INVALID_PROFILE")
	ErrSeatTaken       = errors.New("NETPLAY_SEAT_TAKEN")
	ErrRoomNotReady    = errors.New("NETPLAY_ROOM_NOT_READY")
	ErrRoomConflict    = errors.New("NETPLAY_ROOM_STATE_CONFLICT")
	ErrProfileStale    = errors.New("NETPLAY_PROFILE_STALE")
	ErrCapacity        = errors.New("NETPLAY_CAPACITY_REACHED")
	ErrPrecondition    = errors.New("PRECONDITION_FAILED")
	errUUIDUnavailable = errors.New("netplay: UUID unavailable")
	errEventData       = errors.New("netplay: event data invalid")
)

func serviceError(operation string, err error) error {
	return fmt.Errorf("netplay/%s: %w", operation, err)
}

type Options struct {
	MaxActiveRooms int
	DraftIdle      time.Duration
	WaitingIdle    time.Duration
	ReconnectLease time.Duration
}

type Service struct {
	database    *sql.DB
	registry    *Registry
	credentials *Credentials
	clock       Clock
	options     Options
	stop        chan struct{}
	done        chan struct{}
	launchMu    sync.Mutex
	tags        *tagging.Service
}

const (
	RoomDispositionWaiting = "WAITING"
	RoomDispositionEnded   = "ENDED"
)

func endDisposition(reason string, actorIsHost bool) string {
	switch reason {
	case "HOST_CLOSED", "HOST_LOST", "PROFILE_REVOKED", "SERVER_RESTARTED", "RESTORE", "HARD_EXPIRED":
		return RoomDispositionEnded
	case "AUTH_REVOKED", "PEER_TIMEOUT", "PROTOCOL_VIOLATION":
		if actorIsHost {
			return RoomDispositionEnded
		}
	}
	return RoomDispositionWaiting
}

type resyncCause string

const (
	resyncReconnect resyncCause = "PEER_RECONNECTED"
	resyncHash      resyncCause = "STATE_MISMATCH"
	resyncHost      resyncCause = "HOST_RESUME"
)

func NewService(
	database *sql.DB,
	registry *Registry,
	credentials *Credentials,
	options Options,
	now func() time.Time,
) *Service {
	clock := Clock(realClock{})
	if now != nil {
		clock = clockFunc(now)
	}
	return &Service{
		database: database, registry: registry, credentials: credentials, clock: clock, options: options,
		tags: tagging.New(database, now), stop: make(chan struct{}), done: make(chan struct{}),
	}
}

func (service *Service) SupportsPlatformTarget(
	platformID, coreID, providerID, targetID string,
) bool {
	return service != nil && service.registry.SupportsPlatformTarget(
		platformID, coreID, providerID, targetID,
	)
}

func (service *Service) StartMaintenance() {
	go func() {
		defer close(service.done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-service.stop:
				return
			case <-ticker.C:
				_ = service.ExpireRooms(context.Background())
			}
		}
	}()
}

func (service *Service) Close() {
	select {
	case <-service.stop:
		return
	default:
		close(service.stop)
		<-service.done
	}
}
