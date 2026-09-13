package gamecontent

import (
	"context"
	"errors"
	"testing"
)

type retirementMemory struct {
	RetirementReader
	owners  []RetirementOwner
	changes []RetirementChange
	failure error
}

func (memory *retirementMemory) Blobs(context.Context, string) ([]string, error) {
	return []string{"old"}, nil
}

func (memory *retirementMemory) Owners(context.Context, string) ([]RetirementOwner, error) {
	return memory.owners, nil
}

func (memory *retirementMemory) References(context.Context, string, RetirementReferenceKind, int) ([]RetirementReference, error) {
	return nil, nil
}

func (memory *retirementMemory) Change(_ context.Context, change RetirementChange) error {
	memory.changes = append(memory.changes, change)
	return memory.failure
}

func (memory *retirementMemory) Remove(context.Context, string, RetirementReferenceKind, []RetirementReference) error {
	return nil
}

func TestContentRetirementKeepsTerminalRuntimeAndSelectedVariant(t *testing.T) {
	memory := &retirementMemory{owners: []RetirementOwner{
		{Kind: RetirementLaunch, ID: "live", State: "ACTIVE", Version: 1},
		{Kind: RetirementLaunch, ID: "finished", State: "FINISHED", Version: 2},
		{Kind: RetirementVariant, ID: "selected", State: "READY", Version: 1},
		{Kind: RetirementVariant, ID: "alternate", State: "READY", Version: 1},
	}}
	impact, err := RetireInScope(t.Context(), RetirementScope{Read: memory, Write: memory}, "game", "selected", 20)
	if err != nil || len(memory.changes) != 2 || len(impact.CandidateBlobIDs) != 1 {
		t.Fatalf("retirement=%+v changes=%+v err=%v", impact, memory.changes, err)
	}
	if memory.changes[0].State != "REVOKED" || memory.changes[1].State != "BLOCKED" {
		t.Fatalf("unexpected decisions: %+v", memory.changes)
	}
}

func TestContentRetirementPreservesMutationCause(t *testing.T) {
	cause := errors.New("retirement write failed")
	memory := &retirementMemory{owners: []RetirementOwner{{Kind: RetirementPlay, ID: "play", State: "ACTIVE", Version: 1}}, failure: cause}
	_, err := RetireInScope(t.Context(), RetirementScope{Read: memory, Write: memory}, "game", "variant", 20)
	if !errors.Is(err, cause) {
		t.Fatalf("retirement lost write cause: %v", err)
	}
}
