package netplay

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	repository "retrom/internal/persistence/netplay"
	application "retrom/internal/service/netplay"

	tagpersistence "retrom/internal/persistence/tagging"

	"retrom/internal/service/tagging"
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
	preparation *application.ParticipantPreparation
	tags        *tagging.Service
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
		preparation: application.NewParticipantPreparation(
			repository.NewParticipantPreparation(database),
			credentials,
			application.NewRoomExit(repository.NewRoomExit(database), options.WaitingIdle, clock.Now),
			clock.Now,
		),
		tags: tagging.New(tagpersistence.New(database), now), stop: make(chan struct{}), done: make(chan struct{}),
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
