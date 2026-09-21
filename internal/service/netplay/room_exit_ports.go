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
type RoomExitReader interface {
	Current(context.Context, string, string) (RoomExitSnapshot, error)
}
type RoomExitWriter interface {
	End(context.Context, RoomEndPlan) error
	Remove(context.Context, RoomRemovalPlan) error
}
type RoomExitScope struct {
	Read  RoomExitReader
	Write RoomExitWriter
}
type RoomExitRepository interface {
	WithExit(context.Context, func(RoomExitScope) error) error
}
