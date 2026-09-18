package launch

import (
	"context"
	"errors"
)

var ErrBlocked = errors.New("LAUNCH_BLOCKED")

type Interval struct {
	Running bool `json:"running"`
	Visible bool `json:"visible"`
	Paused  bool `json:"paused"`
}
type PlayEvent struct {
	ClientSequence     int64     `json:"clientSequence"`
	ClientObservedAtMS int64     `json:"clientObservedAtMs"`
	PreviousInterval   *Interval `json:"previousInterval"`
}
type PlayResult struct {
	PlaySessionID    any    `json:"playSessionId"`
	ClientSequence   int64  `json:"clientSequence"`
	AcceptedDuration int64  `json:"acceptedDurationMs"`
	State            string `json:"state"`
}

type PlaySource struct {
	Ref               SessionRef
	Session           SessionRecord
	ProfileID, GameID string
	Version           int64
	IdleExpiresAtMS   *int64
}
type PlayRecord struct {
	ID, State                                                  string
	Version, LastSequence, LastHeartbeatAtMS, ActiveDurationMS int64
}
type StoredPlayEvent struct {
	Kind                                   string
	ClientObservedAtMS, AcceptedDurationMS int64
	Interval                               Interval
}
type PlayStart struct {
	Source                 PlaySource
	PlayID                 string
	Event                  PlayEvent
	NowMS, IdleExpiresAtMS int64
}
type PlayProgress struct {
	Source                                     PlaySource
	Current                                    PlayRecord
	Event                                      PlayEvent
	Kind                                       string
	AcceptedDurationMS, NowMS, IdleExpiresAtMS int64
}
type PlayFinish struct {
	Source PlaySource
	NowMS  int64
}
type PlaySnapshotReader interface {
	LoadPlaySource(context.Context, string) (PlaySource, bool, error)
	LoadPlayRecord(context.Context, string) (PlayRecord, bool, error)
	LoadPlayEvent(context.Context, string, int64) (StoredPlayEvent, bool, error)
}

type PlayCommitter interface {
	CommitPlayStart(context.Context, PlayStart) error
	CommitPlayProgress(context.Context, PlayProgress) error
	CommitPlayFinish(context.Context, PlayFinish) error
}

type PlayRepository interface {
	PlaySnapshotReader
	PlayCommitter
}
