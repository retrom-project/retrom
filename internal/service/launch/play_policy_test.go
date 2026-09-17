package launch

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/launch"
)

type playMemory struct {
	source                                model.PlaySource
	current                               model.PlayRecord
	stored                                model.StoredPlayEvent
	sourceFound, currentFound, eventFound bool
	reads, writes                         int
	start                                 model.PlayStart
	progress                              model.PlayProgress
	finish                                model.PlayFinish
	failure, commitFailure                error
}

func (memory *playMemory) WithPlay(_ context.Context, work func(model.PlayScope) error) error {
	if err := work(model.PlayScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	return memory.commitFailure
}

func (memory *playMemory) Source(context.Context, string) (model.PlaySource, bool, error) {
	memory.reads++
	return memory.source, memory.sourceFound, memory.failure
}

func (memory *playMemory) Current(context.Context, string) (model.PlayRecord, bool, error) {
	memory.reads++
	return memory.current, memory.currentFound, memory.failure
}

func (memory *playMemory) Event(context.Context, string, int64) (model.StoredPlayEvent, bool, error) {
	memory.reads++
	return memory.stored, memory.eventFound, memory.failure
}

func (memory *playMemory) Start(_ context.Context, plan model.PlayStart) error {
	memory.writes++
	memory.start = plan
	return memory.failure
}

func (memory *playMemory) Progress(_ context.Context, plan model.PlayProgress) error {
	memory.writes++
	memory.progress = plan
	return memory.failure
}

func (memory *playMemory) Finish(_ context.Context, plan model.PlayFinish) error {
	memory.writes++
	memory.finish = plan
	return memory.failure
}

func newPlayMemory() *playMemory {
	return &playMemory{sourceFound: true, source: model.PlaySource{Ref: model.SessionRef{ID: "launch"}, Session: model.SessionRecord{
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
		result, err := newPlayController(memory).RecordPlay(t.Context(), "launch", "valid", "start", model.PlayEvent{ClientObservedAtMS: 1})
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
		event model.PlayEvent
	}{
		{"unknown", model.PlayEvent{}},
		{"start", model.PlayEvent{ClientObservedAtMS: -1}},
		{"start", model.PlayEvent{ClientObservedAtMS: 253402300800000}},
		{"heartbeat", model.PlayEvent{ClientSequence: -1}},
	} {
		memory := newPlayMemory()
		if result, err := newPlayController(memory).RecordPlay(t.Context(), "launch", "valid", test.kind, test.event); !errors.Is(err, model.ErrBlocked) || result.PlaySessionID != nil || memory.reads != 0 {
			t.Fatalf("request=%#v result=%#v reads=%d error=%v", test, result, memory.reads, err)
		}
	}
}
