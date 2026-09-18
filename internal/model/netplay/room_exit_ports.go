package netplay

import "context"

type RoomExitSnapshot struct {
	Room      RoomControlSnapshot
	SessionID *string
}
type RoomEndPlan struct {
	Before                                       RoomExitSnapshot
	ActorID                                      *string
	Reason, Disposition, SessionState, PlayState string
	LeaveProfileID, LeaveReason                  string
	Now, ExpiresAtMS                             int64
	Event                                        []byte
}
type RoomRemovalPlan struct {
	Before   RoomControlSnapshot
	Member   SeatMember
	Reason   string
	Evidence RoomControlEvidence
}
type RoomExitRepository interface {
	LoadRoomExitSnapshot(ctx context.Context, roomID, actorID string) (RoomExitSnapshot, error)
	CommitRoomEnd(ctx context.Context, plan RoomEndPlan) error
	CommitRoomRemoval(ctx context.Context, plan RoomRemovalPlan) error
}
