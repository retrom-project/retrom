package netplay

import (
	"context"
	"sync"
	"time"

	"retrom/internal/model/netplayprofile"

	"retrom/internal/foundation/cleanup"
)

type Options struct {
	MaxActiveRooms                         int
	DraftIdle, WaitingIdle, ReconnectLease time.Duration
}
type Components struct {
	Eligibility *Eligibility
	Queries     *RoomQueries
	Creation    *RoomCreation
	Controls    *RoomControl
	Starter     *SessionStart
	Exit        *RoomExit
	Sessions    *SessionControl
	Maintenance *RoomMaintenance
	Events      *RoomEvents
	Access      *ParticipantAccess
	Preparation *ParticipantPreparation
}
type Service struct {
	components      Components
	registry        *netplayprofile.Registry
	mu              sync.Mutex
	started, closed bool
	stop, done      chan struct{}
}

func NewService(components Components, registry *netplayprofile.Registry) *Service {
	return &Service{components: components, registry: registry, stop: make(chan struct{}), done: make(chan struct{})}
}

func (service *Service) StartMaintenance() {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.started || service.closed {
		return
	}
	service.started = true
	go service.maintain()
}

func (service *Service) maintain() {
	defer close(service.done)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-service.stop:
			return
		case <-ticker.C:
			cleanup.Error("netplay maintenance", service.ExpireRooms(context.Background()))
		}
	}
}

func (service *Service) Close() {
	service.mu.Lock()
	if !service.closed {
		service.closed = true
		close(service.stop)
	}
	started := service.started
	service.mu.Unlock()
	if started {
		<-service.done
	}
}
