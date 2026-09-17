package pegasusimport

import (
	"context"
	"errors"
	model "retrom/internal/model/pegasusimport"
	"testing"
	"time"

	"retrom/internal/model/tagging"
)

type mappingMemory struct {
	before                       model.Summary
	owner                        string
	target                       *model.MappingTarget
	readErr, writeErr, commitErr error
	writes                       []model.CollectionMapping
	advance                      *model.MappingAdvance
	scopes                       int
}

func (m *mappingMemory) WithMappings(_ context.Context, work func(model.MappingScope) error) error {
	m.scopes++
	if err := work(model.MappingScope{Read: m, Write: m}); err != nil {
		return err
	}
	return m.commitErr
}

func (m *mappingMemory) Import(context.Context, string) (model.Summary, error) {
	return m.before, m.readErr
}

func (m *mappingMemory) CollectionOwner(context.Context, string) (string, error) {
	return m.owner, m.readErr
}

func (m *mappingMemory) EligibleTarget(context.Context, string) (model.MappingTarget, bool, error) {
	if m.target == nil {
		return model.MappingTarget{}, false, m.readErr
	}
	return *m.target, true, m.readErr
}

func (m *mappingMemory) Put(_ context.Context, change model.CollectionMapping) error {
	m.writes = append(m.writes, change)
	return m.writeErr
}

func (m *mappingMemory) Advance(_ context.Context, change model.MappingAdvance) error {
	m.advance = &change
	m.before.Version++
	return m.writeErr
}

type mappingTagMemory struct {
	actor      string
	ids        []string
	references []tagging.Reference
	err        error
}

func (m *mappingTagMemory) ReplacePegasusCollectionTags(_ context.Context, _ tagging.WriteScope, _ string, ids []string, actor string, _ int64) ([]tagging.Reference, error) {
	m.ids = ids
	m.actor = actor
	return m.references, m.err
}

func mappingFixture() (*mappingMemory, *mappingTagMemory) {
	return &mappingMemory{before: model.Summary{ID: "import", State: "AWAITING_MAPPING", Version: 4, MappingVersion: 3, CreatedBy: model.CreatedBy{ID: "creator"}}, owner: "import", target: &model.MappingTarget{InstanceID: "instance", InstanceVersion: 2, PlatformID: "gba", CoreID: "mgba", ProviderID: "provider", TargetID: "target"}}, &mappingTagMemory{references: []tagging.Reference{}}
}

func TestMappingsUseCurrentSelectionAndActorWithinOneWriteScope(t *testing.T) {
	t.Parallel()
	m, tags := mappingFixture()
	value, err := NewMappings(m, tags, func() time.Time { return time.UnixMilli(10) }).Update(t.Context(), "import", 4, []model.Mapping{{CollectionID: "collection", Action: "IMPORT", PlatformInstanceID: "instance", TagIDs: []string{}}}, "editor")
	if err != nil {
		t.Fatal(err)
	}
	if value.Version != 5 || m.scopes != 1 || len(m.writes) != 1 || m.advance == nil {
		t.Fatalf("mapping result: %#v scope=%d writes=%#v", value, m.scopes, m.writes)
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
		m, tags := mappingFixture()
		value, err := NewMappings(m, tags, time.Now).Update(t.Context(), "import", 4, mappings, "editor")
		if !errors.Is(err, model.ErrInvalid) || value.ID != "" || m.scopes != 0 {
			t.Fatalf("invalid mapping touched storage: %#v %v scopes=%d", mappings, err, m.scopes)
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
			_, err := NewMappings(m, tags, time.Now).Update(t.Context(), "import", 4, []model.Mapping{{CollectionID: "collection", Action: "IMPORT", PlatformInstanceID: "instance", TagIDs: []string{}}}, "editor")
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
			value, err := NewMappings(m, tags, time.Now).Update(t.Context(), "import", 4, []model.Mapping{{CollectionID: "collection", Action: "SKIP", TagIDs: []string{}}}, "")
			if !errors.Is(err, cause) || value.ID != "" {
				t.Fatalf("partial mapping response: %#v %v", value, err)
			}
		})
	}
}

func TestSkippedMappingClearsSelectionAndUsesCreatorFallback(t *testing.T) {
	t.Parallel()
	m, tags := mappingFixture()
	if _, err := NewMappings(m, tags, time.Now).Update(t.Context(), "import", 4, []model.Mapping{{CollectionID: "collection", Action: "SKIP", TagIDs: []string{}}}, ""); err != nil {
		t.Fatal(err)
	}
	if m.writes[0].Target != nil || m.writes[0].Tags == nil || tags.actor != "creator" {
		t.Fatalf("skip selection: %#v actor=%s", m.writes, tags.actor)
	}
}
