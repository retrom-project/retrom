package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"

	"retrom/internal/model/tagging"
)

type mappingMemory struct {
	tags                              *mappingTagMemory
	before                            model.Summary
	owner                             string
	target                            *model.MappingTarget
	readErr, writeErr, commitErr      error
	writes                            []model.CollectionMapping
	advance                           *model.MappingAdvance
	commits                           int
	gameCount                         int64
	ownerErr, advanceErr, responseErr error
	reads                             int
}

func (m *mappingMemory) LoadImportSummary(_ context.Context, _ string) (model.Summary, error) {
	m.reads++
	return m.before, m.readErr
}

func (m *mappingMemory) LoadMappingCollection(_ context.Context, _ string) (model.MappingCollection, error) {
	return model.MappingCollection{ImportID: m.owner, GameCount: m.gameCount}, m.ownerErr
}

func (m *mappingMemory) CommitMappingBatch(ctx context.Context, batch model.MappingBatch) (model.Summary, error) {
	m.commits++
	for _, entry := range batch.Entries {
		if entry.Change.Mapping.Action == "IMPORT" && m.target == nil {
			return model.Summary{}, model.ErrInvalid
		}
	}
	for _, entry := range batch.Entries {
		if entry.Change.Mapping.Action == "IMPORT" {
			entry.Change.Target = m.target
		}
		_, references, err := m.tags.ReplaceOwnerReferences(ctx, entry.Owner, entry.TagIDs, entry.ActorID, entry.Change.NowMS)
		if errors.Is(err, tagging.ErrInvalid) {
			return model.Summary{}, fmt.Errorf("%w: %w", model.ErrInvalid, err)
		}
		if err != nil {
			return model.Summary{}, err
		}
		entry.Change.Tags = references
		m.writes = append(m.writes, entry.Change)
		if m.writeErr != nil {
			return model.Summary{}, m.writeErr
		}
	}
	if m.advanceErr != nil {
		return model.Summary{}, m.advanceErr
	}
	m.advance = &batch.Advance
	m.before.Version++
	m.before.MappingVersion++
	if m.commitErr != nil {
		return model.Summary{}, m.commitErr
	}
	return m.before, m.responseErr
}

type mappingTagMemory struct {
	actor      string
	ids        []string
	references []tagging.Reference
	err        error
}

func (m *mappingTagMemory) ValidateActiveReferences(_ context.Context, ids []string) ([]tagging.Reference, error) {
	refs := make([]tagging.Reference, len(ids))
	for i, id := range ids {
		refs[i] = tagging.Reference{TagID: id}
	}
	return refs, nil
}

func (m *mappingTagMemory) ReplaceOwnerReferences(
	_ context.Context, _ tagging.Owner, ids []string, actor string, _ int64,
) ([]tagging.Reference, []tagging.Reference, error) {
	m.ids = ids
	m.actor = actor
	return nil, m.references, m.err
}

func (m *mappingTagMemory) AssignReferences(
	_ context.Context, _ tagging.Owner, _ []tagging.Reference, _ string, _ int64,
) error {
	return nil
}

func (m *mappingTagMemory) ReadOwnerReferences(
	_ context.Context, _ tagging.Owner,
) ([]tagging.Reference, error) {
	return nil, nil
}

func (m *mappingTagMemory) CopyOwnerReferences(
	_ context.Context, _, _ tagging.Owner, _ string, _ int64,
) ([]tagging.Reference, error) {
	return nil, nil
}

func mappingFixture() (*mappingMemory, *mappingTagMemory) {
	tags := &mappingTagMemory{references: []tagging.Reference{}}
	return &mappingMemory{tags: tags, before: model.Summary{ID: "import", State: "AWAITING_MAPPING", Version: 4, MappingVersion: 3, CreatedBy: model.CreatedBy{ID: "creator"}}, owner: "import", gameCount: 1, target: &model.MappingTarget{InstanceID: "instance", InstanceVersion: 2, PlatformID: "gba", CoreID: "mgba", ProviderID: "provider", TargetID: "target"}}, tags
}

func TestMappingsUseCurrentSelectionAndActorWithinOneWriteScope(t *testing.T) {
	t.Parallel()
	m, tags := mappingFixture()
	value, err := NewMappings(m, func() time.Time { return time.UnixMilli(10) }).Update(t.Context(), "import", 4, []model.Mapping{{CollectionID: "collection", Action: "IMPORT", PlatformInstanceID: "instance", TagIDs: []string{}}}, "editor")
	if err != nil {
		t.Fatal(err)
	}
	if value.Version != 5 || m.commits != 1 || len(m.writes) != 1 || m.advance == nil {
		t.Fatalf("mapping result: %#v commits=%d writes=%#v", value, m.commits, m.writes)
	}
	change := m.writes[0]
	if change.Target == nil || change.Target.InstanceVersion != 2 || change.Target.CoreID != "mgba" || change.NowMS != 10 || tags.actor != "editor" {
		t.Fatalf("mapping snapshot: %#v actor=%s", change, tags.actor)
	}
	if m.advance.Before.Version != 4 || m.advance.Before.MappingVersion != 3 {
		t.Fatalf("mapping fence: %#v", m.advance)
	}
}

func TestMappingsRejectInvalidBatchBeforeStorage(t *testing.T) {
	t.Parallel()
	for _, mappings := range [][]model.Mapping{
		nil, make([]model.Mapping, 101),
		{{CollectionID: "collection", Action: "SKIP"}},
		{{CollectionID: "collection", Action: "UNKNOWN", TagIDs: []string{}}},
		{{CollectionID: "collection", Action: "IMPORT", TagIDs: []string{}}},
		{{CollectionID: "collection", Action: "SKIP", PlatformInstanceID: "instance", TagIDs: []string{}}},
		{{CollectionID: "collection", Action: "SKIP", TagIDs: []string{"tag"}}},
		{{CollectionID: "collection", Action: "SKIP", TagIDs: []string{}}, {CollectionID: "collection", Action: "SKIP", TagIDs: []string{}}},
	} {
		m, _ := mappingFixture()
		value, err := NewMappings(m, time.Now).Update(t.Context(), "import", 4, mappings, "editor")
		if !errors.Is(err, model.ErrInvalid) || value.ID != "" || m.commits != 0 {
			t.Fatalf("invalid mapping touched storage: %#v %v commits=%d", mappings, err, m.commits)
		}
	}
}

func TestMappingsRejectStaleForeignAndUnavailableSelections(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"version", "state", "owner", "target"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			m, tags := mappingFixture()
			want := model.ErrInvalid
			switch reason {
			case "version":
				m.before.Version++
				want = model.ErrVersionConflict
			case "state":
				m.before.State = "RUNNING"
				want = model.ErrMapping
			case "owner":
				m.owner = "other"
			case "target":
				m.target = nil
			}
			_, err := NewMappings(m, time.Now).Update(t.Context(), "import", 4, []model.Mapping{{CollectionID: "collection", Action: "IMPORT", PlatformInstanceID: "instance", TagIDs: []string{}}}, "editor")
			if !errors.Is(err, want) || len(m.writes) != 0 || tags.actor != "" {
				t.Fatalf("invalid %s mutated mappings: %v", reason, err)
			}
		})
	}
}

func TestMappingsPreserveReadTagWriteAndCommitCauses(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"read", "tag", "write", "commit"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			m, tags := mappingFixture()
			cause := errors.New("mapping failure")
			switch phase {
			case "read":
				m.readErr = cause
			case "tag":
				tags.err = cause
			case "write":
				m.writeErr = cause
			case "commit":
				m.commitErr = cause
			}
			value, err := NewMappings(m, time.Now).Update(t.Context(), "import", 4, []model.Mapping{{CollectionID: "collection", Action: "SKIP", TagIDs: []string{}}}, "")
			if !errors.Is(err, cause) || value.ID != "" {
				t.Fatalf("partial mapping response: %#v %v", value, err)
			}
		})
	}
}

func TestSkippedMappingClearsSelectionAndUsesCreatorFallback(t *testing.T) {
	t.Parallel()
	m, tags := mappingFixture()
	if _, err := NewMappings(m, time.Now).Update(t.Context(), "import", 4, []model.Mapping{{CollectionID: "collection", Action: "SKIP", TagIDs: []string{}}}, ""); err != nil {
		t.Fatal(err)
	}
	if m.writes[0].Target != nil || m.writes[0].Tags == nil || tags.actor != "creator" {
		t.Fatalf("skip selection: %#v actor=%s", m.writes, tags.actor)
	}
}
