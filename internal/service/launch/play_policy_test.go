package launch

import (
	"context"
	"time"
)

type playMemory struct {
	source                    PlaySource
	current                   PlayRecord
	sourceFound, currentFound bool
	reads, writes             int
	snapshot                  PlaySnapshotPlan
	failure, commitFailure    error
}

func (memory *playMemory) WithPlay(_ context.Context, work func(PlayScope) error) error {
	if err := work(PlayScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	return memory.commitFailure
}

func (memory *playMemory) Source(context.Context, string) (PlaySource, bool, error) {
	memory.reads++
	return memory.source, memory.sourceFound, memory.failure
}

func (memory *playMemory) Current(context.Context, string) (PlayRecord, bool, error) {
	memory.reads++
	return memory.current, memory.currentFound, memory.failure
}

func (memory *playMemory) Snapshot(_ context.Context, plan PlaySnapshotPlan) error {
	memory.writes++
	memory.snapshot = plan
	return memory.failure
}

func newPlayMemory() *playMemory {
	return &playMemory{sourceFound: true, source: PlaySource{LaunchID: "launch", Session: SessionRecord{
		State: "ACTIVE", CredentialHash: []byte("hash"), HardExpiresAtMS: 1_000_000,
	}, ProfileID: "profile", GameID: "game"}}
}
func playClock() time.Time { return time.UnixMilli(100_000) }
func newPlayController(memory *playMemory) *PlayController {
	service := NewPlayController(memory, playClock, matchTestCapability)
	service.newID = func() (string, error) { return "play", nil }
	return service
}
