package netplay

import (
	"context"
	"time"
)

type HubSessionService interface {
	SetSessionState(context.Context, string, string, string, string) error
	PrepareReconnectResync(context.Context, string, string) error
	PrepareHashResync(context.Context, string, string) error
	PrepareHostResync(context.Context, string, string) error
	MarkSessionRunning(context.Context, string, string) error
}
type HubPeerService interface {
	MarkRuntimeReady(context.Context, SocketParticipant) (bool, error)
	MarkDisconnected(context.Context, SocketParticipant) error
}
type HubTerminationService interface {
	EndRoom(context.Context, string, string, string, *int64) error
	EndSystem(context.Context, string, string) error
}
type HubServices struct {
	Sessions    HubSessionService
	Peers       HubPeerService
	Termination HubTerminationService
}
type HubOptions struct {
	ReconnectLease time.Duration
	Now            func() time.Time
}
