package launch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type projectIndexMemory struct {
	snapshot ProjectIndexSnapshot
	cause    error
	after    func()
	reads    int
}

func (memory *projectIndexMemory) ReadProjectIndex(_ context.Context, _ ProjectIndexReference, authorize ConfigAuthorization) (ProjectIndexSnapshot, bool, error) {
	if memory.cause != nil {
		return ProjectIndexSnapshot{}, false, memory.cause
	}
	if err := authorize(memory.snapshot.Source); err != nil {
		return ProjectIndexSnapshot{}, false, err
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
	return &projectIndexMemory{snapshot: ProjectIndexSnapshot{
		Source: ConfigSource{
			Purpose: "PRODUCT", State: "ACTIVE", Version: 1, HardEnd: 2000, Delivery: "FILE_TREE_PROJECT", ContentKind: "NXENGINE_PROJECT",
			DependencyJSON: `{"schemaVersion":1,"nxengine":{"markerPath":"Doukutsu.exe","compatibility":"NXENGINE_RUNTIME_TRIAL_REQUIRED"}}`,
		},
		Files: []ProjectIndexRecord{{Content: ConfigFile{LogicalName: "Doukutsu.exe", Format: "NXENGINE_PROJECT", Digest: strings.Repeat("a", 64), Size: 16, Role: "GAME"}}},
	}}
}

func TestProjectIndexesRetainsReadFailure(t *testing.T) {
	t.Parallel()
	memory := indexMemoryFixture()
	memory.cause = context.Canceled
	result, err := projectIndexService(memory, func() time.Time { return time.UnixMilli(1000) }).Index(t.Context(), ProjectIndexReference{ID: "launch"}, "valid")
	if !errors.Is(err, context.Canceled) || len(result.Contents) != 0 || result.SHA256 != "" {
		t.Fatalf("read cause lost: %v", err)
	}
}

func TestProjectIndexesAuthorizesBeforeFilesAndChecksExpiryAfterRead(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1000)
	memory := indexMemoryFixture()
	service := projectIndexService(memory, func() time.Time { return now })
	if _, err := service.Index(t.Context(), ProjectIndexReference{ID: "launch"}, "wrong"); !errors.Is(err, ErrCredential) || memory.reads != 0 {
		t.Fatalf("unauthorized files read: %d %v", memory.reads, err)
	}
	memory.after = func() { now = time.UnixMilli(2000) }
	result, err := service.Index(t.Context(), ProjectIndexReference{ID: "launch"}, "valid")
	if !errors.Is(err, ErrCredential) || len(result.Contents) != 0 {
		t.Fatalf("expired read published: %v", err)
	}
}
