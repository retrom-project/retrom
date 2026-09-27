package launch

import (
	"context"
	"errors"
)

var ErrBlocked = errors.New("LAUNCH_BLOCKED")

type PlaySnapshot struct {
	ActiveDurationMS int64 `json:"activeDurationMs"`
}
type PlaySnapshotResult struct {
	PlaySessionID    string `json:"playSessionId"`
	ActiveDurationMS int64  `json:"activeDurationMs"`
}

type PlaySource struct {
	LaunchID          string
	Session           SessionRecord
	ProfileID, GameID string
}
type PlayRecord struct {
	ID, State                 string
	Version, ActiveDurationMS int64
}
type PlaySnapshotPlan struct {
	Source           PlaySource
	Current          *PlayRecord
	PlayID           string
	ActiveDurationMS int64
	NowMS            int64
}
type PlayReader interface {
	Source(context.Context, string) (PlaySource, bool, error)
	Current(context.Context, string) (PlayRecord, bool, error)
}
type PlayWriter interface {
	Snapshot(context.Context, PlaySnapshotPlan) error
}
type PlayScope struct {
	Read  PlayReader
	Write PlayWriter
}
type PlayRepository interface {
	WithPlay(context.Context, func(PlayScope) error) error
}
