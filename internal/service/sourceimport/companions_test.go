package sourceimport

import (
	"context"
	"errors"
	"testing"
	"time"
)

type companionMemory struct {
	before       OwnedItem
	dependencies []string
	candidates   []CompanionCandidate
	registered   bool
	failure      error
}

func (memory *companionMemory) WithCompanions(_ context.Context, work func(CompanionScope) error) error {
	return work(CompanionScope{Read: memory, Write: memory})
}

func (memory *companionMemory) Owner(context.Context, string) (OwnedItem, error) {
	return memory.before, memory.failure
}

func (memory *companionMemory) Dependencies(context.Context, string, string) ([]string, error) {
	return memory.dependencies, memory.failure
}

func (memory *companionMemory) Candidates(context.Context, ExecutionItem) ([]CompanionCandidate, error) {
	return memory.candidates, memory.failure
}

func (memory *companionMemory) Register(context.Context, CompanionRegistration) (string, error) {
	memory.registered = true
	return "companion-blob", memory.failure
}

func TestCompanionPolicySelectsOnlyRequiredArchivesAndRechecksFrozenFacts(t *testing.T) {
	t.Parallel()
	item, id := itemWorkFixture()
	item.item.State = "COPYING"
	item.item.TargetDATVersionID = "dat"
	item.item.Files = []ExecutionFile{{Path: "child.zip"}}
	candidate := CompanionCandidate{
		ItemID: "parent",
		File:   ExecutionFile{Path: "sub/parent.ZIP", Facts: "frozen", Size: 4},
	}
	memory := &companionMemory{
		before:       OwnedItem{Execution: item.execution, Item: item.item},
		dependencies: []string{"parent"},
		candidates: []CompanionCandidate{
			candidate,
			{ItemID: "unsupported", File: ExecutionFile{Path: "parent.7z"}},
			{ItemID: "unrelated", File: ExecutionFile{Path: "unrelated.zip"}},
		},
	}
	service := NewCompanions(memory, func() time.Time { return time.UnixMilli(10) })
	selected, err := service.Find(t.Context(), id, "item")
	if err != nil || len(selected) != 1 || selected[0] != candidate {
		t.Fatalf("selection=%#v err=%v", selected, err)
	}
	changed := candidate
	changed.File.Facts = "changed"
	blob := VerifiedBlob{SHA256: "sha", Size: 4}
	if result, err := service.Record(
		t.Context(),
		id,
		"item",
		changed,
		blob,
	); result != "" || !errors.Is(
		err,
		ErrVersionConflict,
	) || memory.registered {
		t.Fatalf("changed candidate=%s %v", result, err)
	}
	if result, err := service.Record(
		t.Context(),
		id,
		"item",
		candidate,
		blob,
	); result != "companion-blob" || err != nil || !memory.registered {
		t.Fatalf("candidate=%s %v", result, err)
	}
	memory.registered = false
	memory.failure = errors.New("lookup failure")
	if _, err := service.Find(t.Context(), id, "item"); !errors.Is(err, memory.failure) {
		t.Fatalf("lost read cause: %v", err)
	}
}
