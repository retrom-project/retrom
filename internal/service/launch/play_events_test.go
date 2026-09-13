package launch

import (
	"context"
	"errors"
	"math"
	"testing"
)

func activePlayMemory() *playMemory {
	memory := newPlayMemory()
	memory.currentFound = true
	memory.current = PlayRecord{ID: "play", State: "ACTIVE", Version: 2, LastSequence: 0, LastHeartbeatAtMS: 70_000, ActiveDurationMS: 10}
	return memory
}

func heartbeatEvent() PlayEvent {
	return PlayEvent{ClientSequence: 1, ClientObservedAtMS: 10, PreviousInterval: &Interval{Running: true, Visible: true}}
}

func TestPlayDurationUsesServerTimeAndBillableInterval(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                string
		interval            Interval
		previous, now, want int64
	}{
		{"normal", Interval{Running: true, Visible: true}, 70_000, 100_000, 30_000},
		{"cap", Interval{Running: true, Visible: true}, 1, 100_000, 45_000},
		{"clock reversed", Interval{Running: true, Visible: true}, 100_001, 100_000, 0},
		{"negative prior time", Interval{Running: true, Visible: true}, -1, 1, 2},
		{"overflow", Interval{Running: true, Visible: true}, math.MinInt64, math.MaxInt64, 45_000},
		{"hidden", Interval{Running: true}, 70_000, 100_000, 0},
		{"paused", Interval{Running: true, Visible: true, Paused: true}, 70_000, 100_000, 0},
		{"not running", Interval{Visible: true}, 70_000, 100_000, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := acceptedPlayDuration(test.interval, test.previous, test.now); got != test.want {
				t.Fatalf("accepted=%d want=%d", got, test.want)
			}
		})
	}
}

func TestPlayProgressAndFinishCarryDecisionsIntoOneTransaction(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"heartbeat", "finish"} {
		memory := activePlayMemory()
		event := heartbeatEvent()
		result, err := newPlayController(memory).RecordPlay(t.Context(), "launch", "valid", kind, event)
		expectedState := "ACTIVE"
		if kind == "finish" {
			expectedState = "FINISHED"
		}
		if err != nil || result.PlaySessionID != "play" || result.AcceptedDuration != 30_000 || result.State != expectedState || memory.writes != 1 {
			t.Fatalf("kind=%s result=%#v writes=%d error=%v", kind, result, memory.writes, err)
		}
		plan := memory.progress
		if plan.Kind != kind || plan.Current.Version != 2 || plan.Source.Version != 1 || plan.NowMS != 100_000 || plan.AcceptedDurationMS != 30_000 || plan.Event.ClientObservedAtMS != 10 {
			t.Fatalf("progress plan=%#v", plan)
		}
	}
}

func TestPlayProgressRejectsGapsExpiryAndOverflow(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*playMemory, *PlayEvent){
		"gap":               func(_ *playMemory, event *PlayEvent) { event.ClientSequence = 2 },
		"idle boundary":     func(memory *playMemory, _ *PlayEvent) { now := int64(100_000); memory.source.IdleExpiresAtMS = &now },
		"hard boundary":     func(memory *playMemory, _ *PlayEvent) { memory.source.Session.HardExpiresAtMS = 100_000 },
		"inactive play":     func(memory *playMemory, _ *PlayEvent) { memory.current.State = "FINISHED" },
		"source version":    func(memory *playMemory, _ *PlayEvent) { memory.source.Version = math.MaxInt64 },
		"play version":      func(memory *playMemory, _ *PlayEvent) { memory.current.Version = math.MaxInt64 },
		"duration overflow": func(memory *playMemory, _ *PlayEvent) { memory.current.ActiveDurationMS = math.MaxInt64 },
		"sequence overflow": func(memory *playMemory, event *PlayEvent) {
			memory.current.LastSequence = math.MaxInt64
			event.ClientSequence = math.MinInt64
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			memory := activePlayMemory()
			event := heartbeatEvent()
			change(memory, &event)
			result, err := newPlayController(memory).RecordPlay(t.Context(), "launch", "valid", "heartbeat", event)
			if err == nil || result.PlaySessionID != nil || memory.writes != 0 {
				t.Fatalf("result=%#v writes=%d error=%v", result, memory.writes, err)
			}
		})
	}
}

func TestPlayReplayReturnsStoredIntervalAndRejectsChangedBody(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, kind string
		mutate     func(*playMemory, *PlayEvent)
		allowed    bool
	}{
		{"original", "heartbeat", func(*playMemory, *PlayEvent) {}, true},
		{"changed timestamp", "heartbeat", func(_ *playMemory, event *PlayEvent) { event.ClientObservedAtMS++ }, false},
		{"changed interval", "heartbeat", func(_ *playMemory, event *PlayEvent) { event.PreviousInterval.Paused = true }, false},
		{"changed kind", "finish", func(*playMemory, *PlayEvent) {}, false},
		{"missing prior event", "heartbeat", func(memory *playMemory, _ *PlayEvent) { memory.eventFound = false }, false},
		{"expired credential", "heartbeat", func(memory *playMemory, _ *PlayEvent) { memory.source.Session.HardExpiresAtMS = 100_000 }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			memory := activePlayMemory()
			memory.current.LastSequence = 3
			memory.current.State = "FINISHED"
			memory.source.Session.State = "FINISHED"
			event := heartbeatEvent()
			memory.eventFound = true
			memory.stored = StoredPlayEvent{Kind: "HEARTBEAT", ClientObservedAtMS: 10, AcceptedDurationMS: 123, Interval: *event.PreviousInterval}
			test.mutate(memory, &event)
			result, err := newPlayController(memory).RecordPlay(t.Context(), "launch", "valid", test.kind, event)
			if test.allowed {
				if err != nil || result.AcceptedDuration != 123 || result.State != "ACTIVE" {
					t.Fatalf("replay=%#v error=%v", result, err)
				}
			} else if err == nil || result.PlaySessionID != nil {
				t.Fatalf("altered replay=%#v error=%v", result, err)
			}
			if memory.writes != 0 {
				t.Fatalf("replay wrote %d times", memory.writes)
			}
		})
	}
}

func TestPlayControllerNeverReturnsSuccessBeforeCommit(t *testing.T) {
	t.Parallel()
	cause := errors.New("commit rejected")
	memory := newPlayMemory()
	memory.commitFailure = cause
	result, err := newPlayController(memory).RecordPlay(t.Context(), "launch", "valid", "start", PlayEvent{})
	if !errors.Is(err, cause) || result.PlaySessionID != nil || result.State != "" {
		t.Fatalf("commit failure=%#v error=%v", result, err)
	}
	memory = newPlayMemory()
	memory.failure = context.Canceled
	if result, err := newPlayController(memory).RecordPlay(t.Context(), "launch", "valid", "start", PlayEvent{}); !errors.Is(err, context.Canceled) || result.PlaySessionID != nil || memory.writes != 0 {
		t.Fatalf("cancel failure=%#v error=%v", result, err)
	}
	memory = newPlayMemory()
	controller := newPlayController(memory)
	controller.newID = func() (string, error) { return "", cause }
	if result, err := controller.RecordPlay(t.Context(), "launch", "valid", "start", PlayEvent{}); !errors.Is(err, cause) || result.PlaySessionID != nil || memory.writes != 0 {
		t.Fatalf("identity failure=%#v error=%v", result, err)
	}
}
