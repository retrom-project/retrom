package netplay

import (
	"context"

	validation "retrom/internal/model/corevalidation"
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

type SessionStartWriter interface {
	NextNumber(context.Context, string) (int, error)
	Insert(context.Context, SessionStartPlan) (Room, error)
}

type SessionStartScope struct {
	Read        RoomControlReader
	Write       SessionStartWriter
	Eligibility EligibilityRepository
	BIOS        validation.Repository
}

type SessionStartRepository interface {
	WithStart(context.Context, func(SessionStartScope) error) error
}
