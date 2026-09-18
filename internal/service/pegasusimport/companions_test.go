package pegasusimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/pegasusimport"
)

type companionMemory struct {
	before       model.OwnedItem
	dependencies []string
	candidates   []model.CompanionCandidate
	registered   bool
	failure      error
}

func (memory *companionMemory) LoadCompanionOwner(
	_ context.Context, _ string,
) (model.OwnedItem, error) {
	return memory.before, memory.failure
}

func (memory *companionMemory) LoadDependencies(
	_ context.Context, _, _ string,
) ([]string, error) {
	return memory.dependencies, memory.failure
}

func (memory *companionMemory) LoadCandidates(
	_ context.Context, _ model.ExecutionItem,
) ([]model.CompanionCandidate, error) {
	return memory.candidates, memory.failure
}

func (memory *companionMemory) CommitCompanionRegistration(
	_ context.Context, _ model.CompanionRegistration,
) (string, error) {
	if memory.failure != nil {
		return "", memory.failure
	}
	memory.registered = true
	return "companion-blob", nil
}

func TestCompanionPolicySelectsOnlyRequiredArchivesAndRechecksFrozenFacts(t *testing.T) {
	t.Parallel()
	item, id := itemWorkFixture()
	item.item.State = "COPYING"
	item.item.TargetDATVersionID = "dat"
	item.item.Files = []model.ExecutionFile{{Path: "child.zip"}}
	candidate := model.CompanionCandidate{
		ItemID: "parent",
		File: model.ExecutionFile{
			Path: "sub/parent.ZIP", Facts: "frozen", Size: 4,
		},
	}
	memory := &companionMemory{
		before: model.OwnedItem{
			Execution: item.execution, Item: item.item,
		},
		dependencies: []string{"parent"},
		candidates: []model.CompanionCandidate{
			candidate,
			{ItemID: "unsupported", File: model.ExecutionFile{Path: "parent.7z"}},
			{ItemID: "unrelated", File: model.ExecutionFile{Path: "unrelated.zip"}},
		},
	}
	service := NewCompanions(
		memory, func() time.Time { return time.UnixMilli(10) },
	)
	selected, err := service.Find(t.Context(), id, "item")
	if err != nil || len(selected) != 1 || selected[0] != candidate {
		t.Fatalf("selection=%#v err=%v", selected, err)
	}
	changed := candidate
	changed.File.Facts = "changed"
	blob := model.VerifiedBlob{SHA256: "sha", Size: 4}
	if result, err := service.Record(
		t.Context(), id, "item", changed, blob,
	); result != "" || !errors.Is(err, model.ErrVersionConflict) ||
		memory.registered {
		t.Fatalf("changed candidate=%s %v", result, err)
	}
	if result, err := service.Record(
		t.Context(), id, "item", candidate, blob,
	); result != "companion-blob" || err != nil || !memory.registered {
		t.Fatalf("candidate=%s %v", result, err)
	}
	memory.registered = false
	memory.failure = errors.New("lookup failure")
	if _, err := service.Find(t.Context(), id, "item"); !errors.Is(
		err, memory.failure,
	) {
		t.Fatalf("lost read cause: %v", err)
	}
}
