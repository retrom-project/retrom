package launch

import (
	"context"
	"errors"
	model "retrom/internal/model/launch"
	"strings"
	"testing"
	"time"
)

type projectIndexMemory struct {
	snapshot model.ProjectIndexSnapshot
	cause    error
	after    func()
	reads    int
}

func (memory *projectIndexMemory) ReadProjectIndex(_ context.Context, _ model.ProjectIndexReference, authorize model.ConfigAuthorization) (model.ProjectIndexSnapshot, bool, error) {
	if memory.cause != nil {
		return model.ProjectIndexSnapshot{}, false, memory.cause
	}
	if err := authorize(memory.snapshot.Source); err != nil {
		return model.ProjectIndexSnapshot{}, false, err
	}
	memory.reads++
	if memory.after != nil {
		memory.after()
	}
	return memory.snapshot, true, nil
}

func projectIndexService(memory *projectIndexMemory, now func() time.Time) *ProjectIndexes {
	return NewProjectIndexes(memory, now, func(capability string, _ []byte) bool { return capability == "valid" })
}

func indexMemoryFixture() *projectIndexMemory {
	return &projectIndexMemory{snapshot: model.ProjectIndexSnapshot{
		Source: model.ConfigSource{
			Purpose: "PRODUCT", State: "ACTIVE", Version: 1, HardEnd: 2000, Delivery: "FILE_TREE_PROJECT", ContentKind: "NXENGINE_PROJECT",
			DependencyJSON: `{"schemaVersion":1,"nxengine":{"markerPath":"Doukutsu.exe","compatibility":"NXENGINE_RUNTIME_TRIAL_REQUIRED"}}`,
		},
		Files: []model.ProjectIndexRecord{{Content: model.ConfigFile{LogicalName: "Doukutsu.exe", Format: "NXENGINE_PROJECT", Digest: strings.Repeat("a", 64), Size: 16, Role: "GAME"}}},
	}}
}

func TestProjectIndexesRetainsReadFailure(t *testing.T) {
	t.Parallel()
	memory := indexMemoryFixture()
	memory.cause = context.Canceled
	result, err := projectIndexService(memory, func() time.Time { return time.UnixMilli(1000) }).Index(t.Context(), model.ProjectIndexReference{ID: "launch"}, "valid")
	if !errors.Is(err, context.Canceled) || len(result.Contents) != 0 || result.SHA256 != "" {
		t.Fatalf("read cause lost: %v", err)
	}
}

func TestProjectIndexesAuthorizesBeforeFilesAndChecksExpiryAfterRead(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1000)
	memory := indexMemoryFixture()
	service := projectIndexService(memory, func() time.Time { return now })
	if _, err := service.Index(t.Context(), model.ProjectIndexReference{ID: "launch"}, "wrong"); !errors.Is(err, model.ErrCredential) || memory.reads != 0 {
		t.Fatalf("unauthorized files read: %d %v", memory.reads, err)
	}
	memory.after = func() { now = time.UnixMilli(2000) }
	result, err := service.Index(t.Context(), model.ProjectIndexReference{ID: "launch"}, "valid")
	if !errors.Is(err, model.ErrCredential) || len(result.Contents) != 0 {
		t.Fatalf("expired read published: %v", err)
	}
}
