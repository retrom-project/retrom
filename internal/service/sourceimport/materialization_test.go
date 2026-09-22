package sourceimport

import (
	"context"
	"errors"
	"testing"
	"time"
)

type materialMemory struct {
	snapshot                MaterialSnapshot
	phase                   ExecutionPhase
	binds, warnings, phases int
	failure                 error
}

func (m *materialMemory) WithMaterialization(_ context.Context, work func(MaterialScope) error) error {
	return work(MaterialScope{Read: m, Write: m})
}

func (m *materialMemory) Source(context.Context, MaterialKey) (MaterialSnapshot, error) {
	return m.snapshot, m.failure
}

func (m *materialMemory) Execution(context.Context, string) (ExecutionPhase, error) {
	return m.phase, m.failure
}

func (m *materialMemory) Bind(_ context.Context, _ MaterialBinding) (string, error) {
	m.binds++
	return "blob", m.failure
}

func (m *materialMemory) Warn(_ context.Context, _ MaterialWarning) error {
	m.warnings++
	return m.failure
}

func (m *materialMemory) Phase(_ context.Context, _ PhaseChange) error { m.phases++; return m.failure }

func materialMemoryFixture() (*materialMemory, ExecutionIdentity, MaterialSource, VerifiedBlob) {
	item, id := itemWorkFixture()
	item.item.State = "COPYING"
	source := MaterialSource{Key: MaterialKey{ItemID: "item", Ordinal: 0}, Path: "game.gba", Facts: "facts", Size: 4}
	blob := VerifiedBlob{SHA256: "digest", Size: 4}
	return &materialMemory{
		snapshot: MaterialSnapshot{
			Before: OwnedItem{Execution: item.execution, Item: item.item},
			Source: source,
			State:  "DISCOVERED",
		},
		phase: ExecutionPhase{Execution: item.execution, Phase: "COPYING_CONTENT"},
	}, id, source, blob
}

func TestMaterializationPolicyFencesWritesAndCopiedReplay(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"worker", "size", "path", "facts", "state", "replay mismatch"} {
		t.Run(field, func(t *testing.T) {
			memory, id, source, blob := materialMemoryFixture()
			switch field {
			case "worker":
				id.WorkerID = "previous"
			case "size":
				blob.Size++
			case "path":
				source.Path = "different"
			case "facts":
				source.Facts = "changed"
			case "state":
				memory.snapshot.Before.Item.State = "VALIDATING"
			case "replay mismatch":
				memory.snapshot.State = "COPIED"
				memory.snapshot.Blob = VerifiedBlob{SHA256: "other", Size: 4}
				memory.snapshot.BlobID = "other"
			}
			service := NewMaterialization(memory, func() time.Time { return time.UnixMilli(10) })
			result, err := service.Copy(t.Context(), id, source, blob)
			if err == nil || result != "" || memory.binds != 0 {
				t.Fatalf("%s wrote: %s %v binds=%d", field, result, err, memory.binds)
			}
		})
	}
	memory, id, source, blob := materialMemoryFixture()
	memory.snapshot.State = "COPIED"
	memory.snapshot.Blob = blob
	memory.snapshot.BlobID = "existing"
	service := NewMaterialization(memory, func() time.Time { return time.UnixMilli(10) })
	if result, err := service.Copy(
		t.Context(),
		id,
		source,
		blob,
	); err != nil || result != "existing" || memory.binds != 0 {
		t.Fatalf("replay=%s %v", result, err)
	}
}

func TestMaterializationPhaseAndCancellationPreserveAuthorityAndReadCause(t *testing.T) {
	t.Parallel()
	memory, id, _, _ := materialMemoryFixture()
	service := NewMaterialization(memory, func() time.Time { return time.UnixMilli(10) })
	if err := service.SetPhase(t.Context(), id, "COPYING_CONTENT"); err != nil || memory.phases != 0 {
		t.Fatalf("same phase=%v", err)
	}
	if err := service.SetPhase(t.Context(), id, "unknown"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown phase=%v", err)
	}
	cause := errors.New("storage unavailable")
	memory.failure = cause
	if cancelled, err := service.Cancelled(t.Context(), id); cancelled || !errors.Is(err, cause) {
		t.Fatalf("read failure=%v %v", cancelled, err)
	}
	memory.failure = nil
	memory.phase.Execution.JobState = "CANCEL_REQUESTED"
	memory.phase.Execution.ImportState = "CANCEL_REQUESTED"
	if cancelled, err := service.Cancelled(t.Context(), id); !cancelled || err != nil {
		t.Fatalf("cancel checkpoint=%v %v", cancelled, err)
	}
	id.WorkerID = "previous"
	if cancelled, err := service.Cancelled(t.Context(), id); cancelled || !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale checkpoint=%v %v", cancelled, err)
	}
}
