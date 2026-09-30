package cleanupjobs

import (
	"context"
	"errors"
	"testing"

	jobs "retrom/internal/service/cleanupjobs"
	gamecleanup "retrom/internal/service/gamecontent/payloadpolicy"
	importcleanup "retrom/internal/service/libraryimport/payloadpolicy"
	sourcecleanup "retrom/internal/service/sourceimport/payloadpolicy"
)

type effectGraphMemory struct {
	owners map[jobs.Scope]jobs.EffectOwner
	links  map[jobs.Scope][]jobs.Scope
	writes int
}

func (memory *effectGraphMemory) Owner(_ context.Context, scope jobs.Scope) (jobs.EffectOwner, error) {
	return memory.owners[scope], nil
}

func (*effectGraphMemory) Payload(context.Context, jobs.Scope) (jobs.EffectPayload, error) {
	return jobs.EffectPayload{}, nil
}

func (memory *effectGraphMemory) Links(_ context.Context, scope jobs.Scope) ([]jobs.Scope, error) {
	return memory.links[scope], nil
}
func (*effectGraphMemory) Remaining(context.Context, jobs.Scope) (int64, error) { return 0, nil }
func (*effectGraphMemory) Mutations(context.Context, jobs.Scope) (int64, error) { return 0, nil }
func (memory *effectGraphMemory) ChangeOwner(_ context.Context, change jobs.EffectOwnerChange) error {
	if memory.owners[change.Before.Owner.Scope] != change.Before {
		return jobs.ErrEffectConflict
	}
	after := change.Before
	after.Owner = change.After
	memory.owners[change.After.Scope] = after
	memory.writes++
	return nil
}
func (*effectGraphMemory) Clear(context.Context, jobs.EffectOwner, int64) error { return nil }
func (*effectGraphMemory) Consume(context.Context, jobs.EffectConsumptionChange) error {
	return errors.New("unexpected consumption")
}

func TestReleaseEffectNeverReleasesAnotherOwnersPayload(t *testing.T) {
	for _, kind := range []jobs.ScopeType{jobs.ScopeGame, jobs.ScopeImportItem, jobs.ScopeSourceImportItem} {
		t.Run(string(kind), func(t *testing.T) {
			root := jobs.EffectOwner{Found: true, Owner: jobs.Owner{Scope: jobs.Scope{Type: kind, ID: "root"}, State: "PUBLISHED", PayloadState: "RELEASING", ReleaseJobID: "root-release", Version: 2}}
			source := jobs.EffectOwner{Found: true, Owner: jobs.Owner{Scope: jobs.Scope{Type: jobs.ScopeSourceImportItem, ID: "source"}, State: "REVIEW_PENDING", PayloadState: "RETAINED", PublicID: "root", Version: 4}}
			root.MetadataSource = jobs.EffectSource{Kind: "IMPORT_RECEIVE"}
			root.ContentSource = root.MetadataSource
			root.Owner.PublicID = "unrelated-import-item"
			memory := &effectGraphMemory{owners: map[jobs.Scope]jobs.EffectOwner{root.Owner.Scope: root, source.Owner.Scope: source}, links: map[jobs.Scope][]jobs.Scope{root.Owner.Scope: {source.Owner.Scope}}}
			scope := jobs.EffectScope{Read: memory, Write: memory}
			if kind == jobs.ScopeGame {
				root.Owner.State = "DELETED"
				memory.owners[root.Owner.Scope] = root
			}
			release := importcleanup.Release
			if kind == jobs.ScopeGame {
				release = gamecleanup.Release
			}
			if kind == jobs.ScopeSourceImportItem {
				release = sourcecleanup.Release
			}
			unit := jobs.Execution{Work: jobs.Work{ID: root.Owner.ReleaseJobID, Scope: root.Owner.Scope}, Input: jobs.Input{Inputs: jobs.ScopeInputs{ScopeVersion: root.Owner.Version}}}
			if _, err := release(t.Context(), scope, unit, 10); err != nil {
				t.Fatal(err)
			}
			if memory.owners[source.Owner.Scope] != source || memory.owners[root.Owner.Scope].Owner.PayloadState != "RELEASED" || memory.writes != 1 {
				t.Fatalf("release crossed owner boundary: %#v", memory)
			}
			if _, err := release(t.Context(), scope, unit, 10); err != nil || memory.writes != 1 {
				t.Fatalf("replay=%v writes=%d", err, memory.writes)
			}
		})
	}
}

func TestReleaseEffectAggregateRejectsProtectedChildren(t *testing.T) {
	parent := jobs.EffectOwner{Found: true, Owner: jobs.Owner{Scope: jobs.Scope{Type: jobs.ScopeImportJob, ID: "parent"}, State: "COMPLETED", PayloadState: "RELEASING", ReleaseJobID: "parent-release", Version: 5}}
	child := jobs.EffectOwner{Found: true, ParentID: "parent", Owner: jobs.Owner{Scope: jobs.Scope{Type: jobs.ScopeImportItem, ID: "child"}, State: "PUBLISHED", PayloadState: "RELEASED", ReleaseJobID: "child-release", Version: 3}}
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
			memory := &effectGraphMemory{owners: map[jobs.Scope]jobs.EffectOwner{child.Owner.Scope: changed}, links: map[jobs.Scope][]jobs.Scope{parent.Owner.Scope: {child.Owner.Scope}}}
			scope := jobs.EffectScope{Read: memory, Write: memory}
			if err := importcleanup.RequireReleasedItems(t.Context(), scope, parent); jobs.WorkErrorCode(err) != "OWNER_CLEANUP_DEPENDENCY_PENDING" || memory.writes != 0 {
				t.Fatalf("child=%s error=%v writes=%d", field, err, memory.writes)
			}
		})
	}
}
