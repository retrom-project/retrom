package netplay

import "context"

type PeerIdentity struct {
	RoomID, SessionID, ProfileID string
	PlayerNo                     int
	CredentialGeneration         int64
}
type SessionPeer struct {
	ProfileID, State              string
	PlayerNo                      int
	CredentialGeneration, Version int64
}
type SessionControlSnapshot struct {
	RoomID, SessionID, HostID, RoomState, State string
	RoomVersion, Version                        int64
	ResyncCount                                 int
	Peers                                       []SessionPeer
}
type SessionEventData struct {
	SchemaVersion int    `json:"schemaVersion"`
	FromState     string `json:"fromState,omitempty"`
	ToState       string `json:"toState,omitempty"`
	Reason        string `json:"reason,omitempty"`
	ResyncCount   *int   `json:"resyncCount,omitempty"`
}
type SessionEvent struct {
	Type     string
	ActorID  *string
	PlayerNo *int
	Data     SessionEventData
}
type PeerTransitionMode string

const (
	PeersUnchanged     PeerTransitionMode = ""
	PeersPrepareResync PeerTransitionMode = "RESYNC"
	PeersConnect       PeerTransitionMode = "CONNECT"
)

type SessionTransitionPlan struct {
	Before                   SessionControlSnapshot
	Target                   string
	IncrementResync, Started bool
	PeerMode                 PeerTransitionMode
	Events                   []SessionEvent
	Now                      int64
}
type PeerTransitionPlan struct {
	Before                             SessionControlSnapshot
	Peer                               SessionPeer
	Target                             string
	DisconnectedAtMS, LeaseExpiresAtMS *int64
	Events                             []SessionEvent
	Now                                int64
}
type SessionControlReader interface {
	Current(context.Context, string, string) (SessionControlSnapshot, error)
}
type SessionControlWriter interface {
	Session(context.Context, SessionTransitionPlan) error
	Peer(context.Context, PeerTransitionPlan) error
}
type SessionControlScope struct {
	Read  SessionControlReader
	Write SessionControlWriter
}
// SetSessionStateCommand transitions the session between PAUSED_RECONNECT and RUNNING.
type SetSessionStateCommand struct {
	RoomID, SessionID, ActorID, Target string
	NowMS                              int64
}

// ResyncCommand initiates a resynchronization.
type ResyncCommand struct {
	RoomID, SessionID string
	Cause             ResyncCause
	NowMS             int64
}

// RunningCommand transitions from SYNCHRONIZING/RESYNCHRONIZING to RUNNING.
type RunningCommand struct {
	RoomID, SessionID string
	NowMS             int64
}

// DisconnectedCommand records a peer disconnection.
type DisconnectedCommand struct {
	Identity PeerIdentity
	NowMS    int64
	LeaseMS  int64
}

// RuntimeReadyCommand records a peer reaching runtime-ready state.
type RuntimeReadyCommand struct {
	Identity PeerIdentity
	NowMS    int64
}

type SessionControlRepository interface {
	CommitSetSessionState(context.Context, SetSessionStateCommand) error
	CommitResync(context.Context, ResyncCommand) error
	CommitRunning(context.Context, RunningCommand) error
	CommitDisconnected(context.Context, DisconnectedCommand) error
	CommitRuntimeReady(context.Context, RuntimeReadyCommand) (bool, error)
}
type ResyncCause string

const (
	ResyncReconnect ResyncCause = "PEER_RECONNECTED"
	ResyncHash      ResyncCause = "STATE_MISMATCH"
	ResyncHost      ResyncCause = "HOST_RESUME"
)
