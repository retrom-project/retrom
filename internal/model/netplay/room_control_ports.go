package netplay

import (
	"context"

	validation "retrom/internal/model/corevalidation"
)

type RoomSelection struct {
	GameID, VariantID, ProfileID, Digest string
	MaxPlayers                           int
}
type SeatMember struct {
	ID, ProfileID, Role string
	PlayerNo            int
	Ready               bool
	Version             int64
	LeftAtMS            *int64
}
type RoomControlSnapshot struct {
	RoomID, HostID, State string
	Version               int64
	Selection             *RoomSelection
	Member                *SeatMember
	Occupants             []SeatMember
}
type RoomControlEvidence struct {
	ActorID, Type    string
	PlayerNo         *int
	Data             []byte
	Now, ExpiresAtMS int64
}
type RoomSelectionPlan struct {
	Before    RoomControlSnapshot
	Selection RoomSelection
	Evidence  RoomControlEvidence
}
type RoomClearPlan struct {
	Before   RoomControlSnapshot
	Evidence RoomControlEvidence
}
type RoomSeatPlan struct {
	Before   RoomControlSnapshot
	MemberID string
	PlayerNo int
	Evidence RoomControlEvidence
}
type RoomReadyPlan struct {
	Before   RoomControlSnapshot
	Ready    bool
	Evidence RoomControlEvidence
}
type RoomControlReader interface {
	Current(context.Context, string, string) (RoomControlSnapshot, error)
	Snapshot(context.Context, string) (Room, error)
}
type RoomControlWriter interface {
	Select(context.Context, RoomSelectionPlan) error
	Clear(context.Context, RoomClearPlan) error
	Seat(context.Context, RoomSeatPlan) error
	Ready(context.Context, RoomReadyPlan) error
}
type RoomControlScope struct {
	Read        RoomControlReader
	Write       RoomControlWriter
	Eligibility EligibilityRepository
	BIOS        validation.Repository
}
// MutationCommand captures the validated room mutation envelope.
type MutationCommand struct {
	RoomID, ActorID string
	Version         int64
	HostOnly        bool
	States          []string
	NowMS           int64
	Apply           func(RoomControlScope, RoomControlSnapshot, int64) error
}

type RoomControlRepository interface {
	CommitMutation(context.Context, MutationCommand) (Room, error)
}
