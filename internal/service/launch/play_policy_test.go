package launch

import (
	"context"
	"errors"
	"testing"
	"time"
)

type playMemory struct {
	source                                PlaySource
	current                               PlayRecord
	stored                                StoredPlayEvent
	sourceFound, currentFound, eventFound bool
	reads, writes                         int
	start                                 PlayStart
	progress                              PlayProgress
	finish                                PlayFinish
	failure, commitFailure                error
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

func (memory *playMemory) Event(context.Context, string, int64) (StoredPlayEvent, bool, error) {
	memory.reads++
	return memory.stored, memory.eventFound, memory.failure
}

func (memory *playMemory) Start(_ context.Context, plan PlayStart) error {
	memory.writes++
	memory.start = plan
	return memory.failure
}

func (memory *playMemory) Progress(_ context.Context, plan PlayProgress) error {
	memory.writes++
	memory.progress = plan
	return memory.failure
}

func (memory *playMemory) Finish(_ context.Context, plan PlayFinish) error {
	memory.writes++
	memory.finish = plan
	return memory.failure
}

func newPlayMemory() *playMemory {
	return &playMemory{sourceFound: true, source: PlaySource{Ref: SessionRef{ID: "launch"}, Session: SessionRecord{
		State: "ACTIVE", CredentialHash: []byte("hash"), HardExpiresAtMS: 1_000_000,
	}, ProfileID: "profile", GameID: "game", Version: 1}}
}
func playClock() time.Time { return time.UnixMilli(100_000) }
func newPlayController(memory *playMemory) *PlayController {
	service := NewPlayController(memory, playClock, matchTestCapability)
	service.newID = func() (string, error) { return "play", nil }
	return service
}

func TestPlayControllerStartsOnlyAfterRuntimeActivation(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"CREATED", "ACTIVE", "FINISHED", "REVOKED", "EXPIRED"} {
		memory := newPlayMemory()
		memory.source.Session.State = state
		result, err := newPlayController(memory).RecordPlay(t.Context(), "launch", "valid", "start", PlayEvent{ClientObservedAtMS: 1})
		if state == "ACTIVE" {
			if err != nil || result.PlaySessionID != "play" || memory.writes != 1 || memory.start.IdleExpiresAtMS != 220_000 {
				t.Fatalf("state=%s result=%#v start=%#v error=%v", state, result, memory.start, err)
			}
		} else if err == nil || result.PlaySessionID != nil || memory.writes != 0 {
			t.Fatalf("inactive start state=%s result=%#v writes=%d error=%v", state, result, memory.writes, err)
		}
	}
}

func TestPlayControllerChecksRequestBeforeOpeningStorage(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		kind  string
		event PlayEvent
	}{
		{"unknown", PlayEvent{}},
		{"start", PlayEvent{ClientObservedAtMS: -1}},
		{"start", PlayEvent{ClientObservedAtMS: 253402300800000}},
		{"heartbeat", PlayEvent{ClientSequence: -1}},
	} {
		memory := newPlayMemory()
		if result, err := newPlayController(memory).RecordPlay(t.Context(), "launch", "valid", test.kind, test.event); !errors.Is(err, ErrBlocked) || result.PlaySessionID != nil || memory.reads != 0 {
			t.Fatalf("request=%#v result=%#v reads=%d error=%v", test, result, memory.reads, err)
		}
	}
}
