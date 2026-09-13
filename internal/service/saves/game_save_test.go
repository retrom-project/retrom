package saves

import (
	"context"
	"errors"
	"testing"
	"time"

	"retrom/internal/adapter/files/blobstore"
)

func TestGameSaveRejectsChangedSlotBeforePublishing(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*gameSaveMemory)
	}{
		{"stale version", func(memory *gameSaveMemory) { memory.saved.DataVersion++ }},
		{"other profile", func(memory *gameSaveMemory) { memory.saved.ProfileID = "other" }},
		{"other game", func(memory *gameSaveMemory) { memory.saved.GameID = "other" }},
		{"other format", func(memory *gameSaveMemory) { memory.saved.Format = "opaque-v0" }},
		{"deleted", func(memory *gameSaveMemory) { stamp := int64(1); memory.saved.DeletedAtMS = &stamp }},
	} {
		t.Run(test.name, func(t *testing.T) {
			memory := gameSaveFixture()
			test.change(memory)
			_, err := New(nil, nil, time.Now).persistProductCheckpoint(t.Context(), memory.scope(), "launch",
				writableLaunch(), parsedGameSave("new"), "payload", 100)
			if !errors.Is(err, ErrSyncConflict) || memory.updated != nil || memory.boundVersion != 0 || memory.images != 0 {
				t.Fatalf("changed slot published: memory=%+v error=%v", memory, err)
			}
		})
	}
}

func TestGameSaveDeduplicatesPayloadAndPreservesUserMetadata(t *testing.T) {
	for _, digest := range []string{"old", "new"} {
		memory := gameSaveFixture()
		result, err := New(nil, nil, time.Now).persistProductCheckpoint(t.Context(), memory.scope(), "launch",
			writableLaunch(), parsedGameSave(digest), "payload", 100)
		if err != nil || result.Name != "user name" || result.CreatedAtMS != 10 {
			t.Fatalf("slot metadata changed: result=%+v error=%v", result, err)
		}
		if digest == "old" {
			if result.Version != 20 || memory.boundVersion != 7 || memory.updated != nil || memory.images != 0 {
				t.Fatalf("identical payload changed versions: result=%+v memory=%+v", result, memory)
			}
		} else {
			assertGameSaveAdvanced(t, result, memory)
		}
	}
}

func assertGameSaveAdvanced(t *testing.T, result ManualResult, memory *gameSaveMemory) {
	t.Helper()
	if result.Version != 21 || memory.boundVersion != 8 || memory.updated == nil ||
		memory.updated.ExpectedDataVersion != 7 || result.ActiveDurationMS != 120 || memory.images != 1 {
		t.Fatalf("update lost fence or duration: result=%+v memory=%+v", result, memory)
	}
}

func parsedGameSave(digest string) parsedManual {
	return parsedManual{
		metadata: manualMetadata{Name: "replacement"}, payload: blobstore.Metadata{SHA256: digest},
		screenshot: &blobstore.Metadata{SHA256: "image"},
	}
}

type gameSaveMemory struct {
	GameSaveRecords
	CheckpointRecords
	BlobRecords
	binding      GameSaveBinding
	saved        StoredSave
	updated      *SaveUpdate
	boundVersion int64
	images       int
}

func gameSaveFixture() *gameSaveMemory {
	id := "save"
	return &gameSaveMemory{
		binding: GameSaveBinding{ID: &id, ExpectedVersion: 7},
		saved: StoredSave{
			ProfileID: "profile", GameID: "game", Format: "opaque-v1", Digest: "old", DataVersion: 7,
			Result: ManualResult{SaveStateID: id, Name: "user name", CreatedAtMS: 10, Version: 20},
		},
	}
}

func (memory *gameSaveMemory) scope() WriteScope {
	return WriteScope{GameSaves: memory, Checkpoints: memory, Blobs: memory}
}

func (memory *gameSaveMemory) Binding(context.Context, string) (GameSaveBinding, bool, error) {
	return memory.binding, true, nil
}

func (memory *gameSaveMemory) Saved(context.Context, string) (StoredSave, bool, error) {
	return memory.saved, true, nil
}

func (memory *gameSaveMemory) UpdateSave(_ context.Context, update SaveUpdate) error {
	memory.updated = &update
	return nil
}

func (memory *gameSaveMemory) Bind(_ context.Context, _, _ string, version int64) error {
	memory.boundVersion = version
	return nil
}

func (memory *gameSaveMemory) Ensure(context.Context, blobstore.Metadata, string, int64) (string, error) {
	memory.images++
	return "image", nil
}

func (memory *gameSaveMemory) Duration(context.Context, string) (Duration, error) {
	return Duration{ActiveMS: 20, InitialMS: 100}, nil
}
