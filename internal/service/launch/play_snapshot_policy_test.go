package launch

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestPlaySnapshotChecksBoundsBeforeStorage(t *testing.T) {
	for _, value := range []int64{-1, maxPlaySnapshotDurationMS + 1, math.MaxInt64} {
		memory := newPlayMemory()
		_, err := newPlayController(memory).RecordSnapshot(t.Context(), "launch", "valid", PlaySnapshot{ActiveDurationMS: value})
		if !errors.Is(err, ErrBlocked) || memory.reads != 0 || memory.writes != 0 {
			t.Fatalf("value=%d error=%v memory=%#v", value, err, memory)
		}
	}
}

func TestPlaySnapshotRejectsInvalidSessionAndCredential(t *testing.T) {
	for name, mutate := range map[string]func(*playMemory){
		"missing":    func(m *playMemory) { m.sourceFound = false },
		"expired":    func(m *playMemory) { m.source.Session.HardExpiresAtMS = playClock().UnixMilli() },
		"launch":     func(m *playMemory) { m.source.LaunchID = "" },
		"profile":    func(m *playMemory) { m.source.ProfileID = "" },
		"game":       func(m *playMemory) { m.source.GameID = "" },
		"credential": func(m *playMemory) { m.source.Session.CredentialHash = nil },
	} {
		t.Run(name, func(t *testing.T) {
			m := newPlayMemory()
			mutate(m)
			if _, err := newPlayController(m).RecordSnapshot(t.Context(), "launch", "valid", PlaySnapshot{}); !errors.Is(err, ErrCredential) || m.writes != 0 {
				t.Fatalf("error=%v writes=%d", err, m.writes)
			}
		})
	}
}

func TestPlaySnapshotPreservesFailuresAndReturnsNoResult(t *testing.T) {
	cause := errors.New("snapshot failure")
	for name, prepare := range map[string]func(*playMemory, *PlayController){
		"read":     func(m *playMemory, _ *PlayController) { m.failure = context.Canceled },
		"commit":   func(m *playMemory, _ *PlayController) { m.commitFailure = cause },
		"identity": func(_ *playMemory, c *PlayController) { c.newID = func() (string, error) { return "", cause } },
	} {
		t.Run(name, func(t *testing.T) {
			m := newPlayMemory()
			c := newPlayController(m)
			prepare(m, c)
			result, err := c.RecordSnapshot(t.Context(), "launch", "valid", PlaySnapshot{})
			expected := cause
			if name == "read" {
				expected = context.Canceled
			}
			if !errors.Is(err, expected) || result.PlaySessionID != "" {
				t.Fatalf("result=%#v error=%v", result, err)
			}
		})
	}
}
