package cleanupjobs

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

func TestReleaseEffectNeverReleasesAnotherOwnersPayload(t *testing.T) {
	for _, kind := range []ScopeType{ScopeGame, ScopeImportItem, ScopeSourceImportItem} {
		t.Run(string(kind), func(t *testing.T) {
			root := EffectOwner{Found: true, Owner: Owner{Scope: Scope{Type: kind, ID: "root"}, State: "PUBLISHED", PayloadState: "RELEASING", ReleaseJobID: "root-release", Version: 2}}
			source := EffectOwner{Found: true, Owner: Owner{Scope: Scope{Type: ScopeSourceImportItem, ID: "source"}, State: "REVIEW_PENDING", PayloadState: "RETAINED", PublicID: "root", Version: 4}}
			root.MetadataSource = EffectSource{Kind: "IMPORT_RECEIVE", ID: "source"}
			root.ContentSource = root.MetadataSource
			root.Owner.PublicID = "unrelated-import-item"
			memory := &effectGraphMemory{owners: map[Scope]EffectOwner{root.Owner.Scope: root, source.Owner.Scope: source}, links: map[Scope][]Scope{root.Owner.Scope: {source.Owner.Scope}}}
			run := effectRun{scope: EffectScope{Read: memory, Write: memory}, visited: make(map[Scope]bool), nowMS: 10}
			if err := run.release(t.Context(), root); err != nil {
				t.Fatal(err)
			}
			if memory.owners[source.Owner.Scope] != source || memory.owners[root.Owner.Scope].Owner.PayloadState != "RELEASED" || memory.writes != 1 {
				t.Fatalf("release crossed owner boundary: %#v", memory)
			}
			if err := run.release(t.Context(), memory.owners[root.Owner.Scope]); err != nil || memory.writes != 1 {
				t.Fatalf("replay=%v writes=%d", err, memory.writes)
			}
		})
	}
}

func TestReleaseEffectAggregateRejectsProtectedChildren(t *testing.T) {
	parent := EffectOwner{Found: true, Owner: Owner{Scope: Scope{Type: ScopeImportJob, ID: "parent"}, State: "COMPLETED", PayloadState: "RELEASING", ReleaseJobID: "parent-release", Version: 5}}
	child := EffectOwner{Found: true, ParentID: "parent", Owner: Owner{Scope: Scope{Type: ScopeImportItem, ID: "child"}, State: "PUBLISHED", PayloadState: "RELEASED", ReleaseJobID: "child-release", Version: 3}}
	for _, field := range []string{"parent", "active", "retained", "releasing", "release-job"} {
		t.Run(field, func(t *testing.T) {
			changed := child
			switch field {
			case "parent":
				changed.ParentID = "other"
			case "active":
				changed.Owner.State = "REVIEW_PENDING"
			case "releasing":
				changed.Owner.PayloadState = "RELEASING"
			case "retained":
				changed.Owner.PayloadState = "RETAINED"
			case "release-job":
				changed.Owner.ReleaseJobID = ""
			}
			memory := &effectGraphMemory{owners: map[Scope]EffectOwner{child.Owner.Scope: changed}, links: map[Scope][]Scope{parent.Owner.Scope: {child.Owner.Scope}}}
			run := effectRun{scope: EffectScope{Read: memory, Write: memory}, visited: make(map[Scope]bool), nowMS: 10}
			if err := run.requireChildren(t.Context(), parent); WorkErrorCode(err) != "OWNER_CLEANUP_DEPENDENCY_PENDING" || memory.writes != 0 {
				t.Fatalf("child=%s error=%v writes=%d", field, err, memory.writes)
			}
		})
	}
}
