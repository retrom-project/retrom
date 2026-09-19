package netplay

import (
	"context"
)

type SessionStartPlan struct {
	Before    RoomControlSnapshot
	SessionID string
	SessionNo int
	Profile   FrozenRoomProfile
	Members   []SeatMember
	SeatMask  int
	Now       int64
	Event     []byte
}

type SessionStartCommand struct {
	RoomID          string
	HostID          string
	ExpectedVersion int64
	FrozenProfile   FrozenRoomProfile
	SessionID       string
	NowMS           int64
	SeatMask        int
	Members         []SeatMember
	Event           []byte
}

type SessionStartRepository interface {
	InspectRoom(context.Context, string, string) (RoomControlSnapshot, error)
	CommitSessionStart(context.Context, SessionStartCommand) (Room, error)
}
