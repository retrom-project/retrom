package payloadrelease

import (
	"context"
	"errors"
	"testing"
)

type effectGraphMemory struct {
	owners map[Scope]EffectOwner
	links  map[Scope][]Scope
	writes int
}

func (memory *effectGraphMemory) Owner(_ context.Context, scope Scope) (EffectOwner, error) {
	return memory.owners[scope], nil
}

func (*effectGraphMemory) Payload(context.Context, Scope) (EffectPayload, error) {
	return EffectPayload{}, nil
}

func (memory *effectGraphMemory) Links(_ context.Context, scope Scope) ([]Scope, error) {
	return memory.links[scope], nil
}
func (*effectGraphMemory) Remaining(context.Context, Scope) (int64, error) { return 0, nil }
func (*effectGraphMemory) Mutations(context.Context, Scope) (int64, error) { return 0, nil }
func (memory *effectGraphMemory) ChangeOwner(_ context.Context, change EffectOwnerChange) error {
	if memory.owners[change.Before.Owner.Scope] != change.Before {
		return ErrEffectConflict
	}
	after := change.Before
	after.Owner = change.After
	memory.owners[change.After.Scope] = after
	memory.writes++
	return nil
}
func (*effectGraphMemory) Remove(context.Context, EffectRemoval) error { return nil }
func (*effectGraphMemory) Consume(context.Context, EffectConsumptionChange) error {
	return errors.New("unexpected consumption")
}

func TestReleaseEffectBoundDuplicateRequiresPermanentProof(t *testing.T) {
	for _, kind := range []ScopeType{ScopeSourceImportItem, ScopeSourceImportItem} {
		t.Run(string(kind), func(t *testing.T) {
			public := EffectOwner{Found: true, Owner: Owner{Scope: Scope{Type: ScopeImportItem, ID: "ordinary"}, State: "DISCARDED", PayloadState: "RELEASING", ReleaseJobID: "ordinary-release", Version: 2}}
			source := EffectOwner{Found: true, ParentID: "plan", ExistingGameID: "game", Owner: Owner{Scope: Scope{Type: kind, ID: "source"}, State: "SKIPPED_EXISTING", PayloadState: "RETAINED", PublicID: "ordinary", Version: 4}}
			memory := &effectGraphMemory{owners: map[Scope]EffectOwner{source.Owner.Scope: source}}
			run := effectRun{scope: EffectScope{Read: memory, Write: memory}, visited: make(map[Scope]bool), nowMS: 10}
			if err := run.boundSource(t.Context(), public, source.Owner.Scope); WorkErrorCode(err) != "PAYLOAD_RELEASE_SOURCE_NOT_TERMINAL" || memory.writes != 0 {
				t.Fatalf("missing proof error=%v writes=%d", err, memory.writes)
			}
			source.DuplicateMatch = true
			memory.owners[source.Owner.Scope] = source
			if err := run.boundSource(t.Context(), public, source.Owner.Scope); err != nil {
				t.Fatal(err)
			}
			after := memory.owners[source.Owner.Scope].Owner
			if after.PayloadState != "RELEASED" || after.Version != 6 || after.ReleaseJobID != "ordinary-release" || memory.writes != 2 {
				t.Fatalf("bound result=%#v writes=%d", after, memory.writes)
			}
			if err := run.boundSource(t.Context(), public, source.Owner.Scope); err != nil || memory.writes != 2 {
				t.Fatalf("replay=%v writes=%d", err, memory.writes)
			}
		})
	}
}

func TestReleaseEffectAggregateRejectsProtectedChildren(t *testing.T) {
	parent := EffectOwner{Found: true, Owner: Owner{Scope: Scope{Type: ScopeImportJob, ID: "parent"}, State: "COMPLETED", PayloadState: "RELEASING", ReleaseJobID: "parent-release", Version: 5}}
	child := EffectOwner{Found: true, ParentID: "parent", Owner: Owner{Scope: Scope{Type: ScopeImportItem, ID: "child"}, State: "PUBLISHED", PayloadState: "RELEASING", ReleaseJobID: "child-release", Version: 3}}
	for _, field := range []string{"parent", "active", "retained", "release-job"} {
		t.Run(field, func(t *testing.T) {
			changed := child
			switch field {
			case "parent":
				changed.ParentID = "other"
			case "active":
				changed.Owner.State = "REVIEW_PENDING"
			case "retained":
				changed.Owner.PayloadState = "RETAINED"
			case "release-job":
				changed.Owner.ReleaseJobID = ""
			}
			memory := &effectGraphMemory{owners: map[Scope]EffectOwner{child.Owner.Scope: changed}, links: map[Scope][]Scope{parent.Owner.Scope: {child.Owner.Scope}}}
			run := effectRun{scope: EffectScope{Read: memory, Write: memory}, visited: make(map[Scope]bool), nowMS: 10}
			if err := run.aggregate(t.Context(), parent); WorkErrorCode(err) != "PAYLOAD_RELEASE_DEPENDENCY_PENDING" || memory.writes != 0 {
				t.Fatalf("child=%s error=%v writes=%d", field, err, memory.writes)
			}
		})
	}
}

func TestReleaseEffectSourceMappingDeduplicatesOnlyIdenticalOwners(t *testing.T) {
	source := EffectSource{Kind: "IMPORT_RECEIVE", ID: "source"}
	links := gameEffectSources(EffectOwner{MetadataSource: source, ContentSource: source})
	if len(links) != 1 || links[0] != (Scope{Type: ScopeSourceImportItem, ID: "source"}) {
		t.Fatalf("deduplicated=%#v", links)
	}
	links = gameEffectSources(EffectOwner{MetadataSource: source, ContentSource: EffectSource{Kind: "IMPORT_REVIEW", ID: "source"}})
	if len(links) != 2 || links[1].Type != ScopeImportItem {
		t.Fatalf("different owners merged=%#v", links)
	}
}
