package emulationstationimport

import (
	"errors"
	"math"
	model "retrom/internal/model/emulationstationimport"
	"testing"
	"time"

	"retrom/internal/model/tagging"
)

func TestMappingsPreserveEveryPortFailureWithoutPartialResponse(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"collection", "target", "advance", "response"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			m, tags := mappingFixture()
			cause := errors.New("typed mapping failure")
			switch phase {
			case "collection":
				m.ownerErr = cause
			case "target":
				m.targetErr = cause
			case "advance":
				m.advanceErr = cause
			case "response":
				m.responseErr = cause
			}
			result, err := NewMappings(m, tags, time.Now).Update(t.Context(), "import", 4, []model.Mapping{{CollectionID: "collection", Action: "IMPORT", PlatformInstanceID: "instance", TagIDs: []string{}}}, "editor")
			if result.ID != "" || !errors.Is(err, cause) {
				t.Fatalf("%s lost cause or leaked result=%#v error=%v", phase, result, err)
			}
		})
	}
}

func TestMappingsRejectVersionOverflowBeforeMutating(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"aggregate", "mapping"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			m, tags := mappingFixture()
			version := m.before.Version
			if kind == "aggregate" {
				m.before.Version = math.MaxInt64
				version = math.MaxInt64
			} else {
				m.before.MappingVersion = math.MaxInt64
			}
			result, err := NewMappings(m, tags, time.Now).Update(t.Context(), "import", version, []model.Mapping{{CollectionID: "collection", Action: "SKIP", TagIDs: []string{}}}, "editor")
			if result.ID != "" || !errors.Is(err, model.ErrVersionConflict) || len(m.writes) != 0 || tags.actor != "" {
				t.Fatalf("overflow mutated mappings: %#v error=%v", result, err)
			}
		})
	}
}

func TestMappingsRejectEmptyImportCollectionButAllowSkip(t *testing.T) {
	t.Parallel()
	m, tags := mappingFixture()
	m.gameCount = 0
	service := NewMappings(m, tags, time.Now)
	_, err := service.Update(t.Context(), "import", 4, []model.Mapping{{CollectionID: "collection", Action: "IMPORT", PlatformInstanceID: "instance", TagIDs: []string{}}}, "editor")
	if !errors.Is(err, model.ErrInvalid) || len(m.writes) != 0 || tags.actor != "" {
		t.Fatalf("empty collection imported: %v", err)
	}
	_, err = service.Update(t.Context(), "import", 4, []model.Mapping{{CollectionID: "collection", Action: "SKIP", TagIDs: []string{}}}, "editor")
	if err != nil || len(m.writes) != 1 {
		t.Fatalf("empty collection could not be skipped: %v", err)
	}
}

func TestMappingsPreserveTagDomainIdentities(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{tagging.ErrInvalid, tagging.ErrReferenceInvalid, tagging.ErrAssignmentLimitExceeded} {
		m, tags := mappingFixture()
		tags.err = cause
		_, err := NewMappings(m, tags, time.Now).Update(t.Context(), "import", 4, []model.Mapping{{CollectionID: "collection", Action: "SKIP", TagIDs: []string{}}}, "editor")
		if !errors.Is(err, cause) || len(m.writes) != 0 {
			t.Fatalf("tag cause lost: %v", err)
		}
		if errors.Is(cause, tagging.ErrInvalid) && !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("invalid tag lost mapping identity: %v", err)
		}
	}
}

func TestMappingsPrepareWholeBatchBeforeAnyTagWrite(t *testing.T) {
	t.Parallel()
	m, tags := mappingFixture()
	m.target = nil
	_, err := NewMappings(m, tags, time.Now).Update(t.Context(), "import", 4, []model.Mapping{
		{CollectionID: "first", Action: "SKIP", TagIDs: []string{}},
		{CollectionID: "second", Action: "IMPORT", PlatformInstanceID: "missing", TagIDs: []string{}},
	}, "editor")
	if !errors.Is(err, model.ErrInvalid) || len(m.writes) != 0 || tags.actor != "" {
		t.Fatalf("partially validated mapping wrote state: %v", err)
	}
}

func TestMappingsShareOneClockSnapshotAndFreezeDATAndTags(t *testing.T) {
	t.Parallel()
	m, tags := mappingFixture()
	dat := "dat-version"
	m.target.DATVersionID = &dat
	tags.references = []tagging.Reference{{TagID: "tag", Name: "Frozen"}}
	reads := 0
	clock := func() time.Time { reads++; return time.UnixMilli(int64(10 + reads)) }
	_, err := NewMappings(m, tags, clock).Update(t.Context(), "import", 4, []model.Mapping{
		{CollectionID: "first", Action: "IMPORT", PlatformInstanceID: "instance", TagIDs: []string{"tag"}},
		{CollectionID: "second", Action: "IMPORT", PlatformInstanceID: "instance", TagIDs: []string{"tag"}},
	}, "editor")
	if err != nil {
		t.Fatal(err)
	}
	if reads != 1 || len(m.writes) != 2 || m.advance.NowMS != 11 {
		t.Fatalf("clock or writes: reads=%d writes=%#v", reads, m.writes)
	}
	for _, change := range m.writes {
		if change.NowMS != 11 || change.Target.DATVersionID == nil || *change.Target.DATVersionID != dat || len(change.Tags) != 1 || change.Tags[0].Name != "Frozen" {
			t.Fatalf("snapshot=%#v", change)
		}
	}
}
