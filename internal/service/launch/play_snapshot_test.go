package launch

import "testing"

func TestPlaySnapshotIsIndependentOfEventSequence(t *testing.T) {
	memory := newPlayMemory()
	controller := newPlayController(memory)
	first, err := controller.RecordSnapshot(t.Context(), "launch", "valid", PlaySnapshot{ActiveDurationMS: 30_000})
	if err != nil || first.PlaySessionID != "play" || first.ActiveDurationMS != 30_000 {
		t.Fatalf("first snapshot=%#v error=%v", first, err)
	}
	memory.currentFound = true
	memory.current = PlayRecord{ID: "play", State: "ACTIVE", Version: 1, ActiveDurationMS: 30_000}
	for _, elapsed := range []int64{20_000, 45_000, 45_000} {
		result, err := controller.RecordSnapshot(t.Context(), "launch", "valid", PlaySnapshot{ActiveDurationMS: elapsed})
		if err != nil || result.ActiveDurationMS != max(30_000, elapsed) {
			t.Fatalf("snapshot %d=%#v error=%v", elapsed, result, err)
		}
	}
	if memory.source.IdleExpiresAtMS != nil {
		t.Fatal("play statistics set a launch idle deadline")
	}
}

func TestPlaySnapshotRejectsInvalidAuthorityWithoutWriting(t *testing.T) {
	for _, state := range []string{"CREATED", "FINISHED", "REVOKED", "EXPIRED"} {
		memory := newPlayMemory()
		memory.source.Session.State = state
		if _, err := newPlayController(memory).RecordSnapshot(t.Context(), "launch", "valid", PlaySnapshot{}); err == nil || memory.writes != 0 {
			t.Fatalf("state=%s writes=%d error=%v", state, memory.writes, err)
		}
	}
}
