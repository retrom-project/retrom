package netplay

import "context"

type FrozenRoomProfile struct {
	Selection                          RoomSelection
	ProviderID, TargetID, BundleSHA256 string
	Canonical                          []byte
}

type RoomCapacity struct {
	Active     int
	HostActive bool
}

type RoomCreationPlan struct {
	RoomID, MemberID, HostID string
	Now, ExpiresAtMS         int64
	Event                    []byte
}

type RoomCreationCommand struct {
	Plan    RoomCreationPlan
	Maximum int
}

type RoomCreationRepository interface {
	CommitRoomCreation(context.Context, RoomCreationCommand) (Room, error)
}

type Event struct {
	ID        int64          `json:"id"`
	EventType string         `json:"eventType"`
	Data      map[string]any `json:"data"`
	CreatedAt int64          `json:"createdAtMs"`
}

type RoomEventPage struct {
	Exists bool
	Events []Event
}

type RoomEventsRepository interface {
	Page(context.Context, string, int64, int) (RoomEventPage, error)
}

type RoomFilter struct {
	ProfileID, View, AfterRoomID    string
	AfterUpdatedAtMS, RecentSinceMS int64
	Limit                           int
}

type RoomQueryRepository interface {
	RoomIDs(context.Context, RoomFilter) ([]string, error)
	Snapshot(context.Context, string) (Room, error)
}

type ExpiryCutoffs struct {
	Now, StartingBefore, RunningBefore int64
	Limit                              int
}

type ExpiryCandidate struct {
	RoomID, HostID, State string
	Version               int64
	SessionID             *string
}

type ExpiryPlan struct {
	Before ExpiryCandidate
	Now    int64
}

type RecoveryPlan struct {
	Reason string
	Now    int64
}

type MaintenanceRepository interface {
	Passive(context.Context, ExpiryCutoffs) ([]ExpiryCandidate, error)
	Active(context.Context, ExpiryCutoffs) ([]ExpiryCandidate, error)
	CommitExpiry(context.Context, ExpiryPlan) error
	CommitRecovery(context.Context, RecoveryPlan) error
}

type ExpiredSessionEnder interface {
	EndExpired(context.Context, ExpiryCandidate, int64) error
}
