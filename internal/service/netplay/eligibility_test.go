package netplay

import (
	"context"
	"errors"
	model "retrom/internal/model/netplay"
	"slices"
	"testing"

	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/model/tagging"
	"retrom/internal/transport/netplay/profile"
)

type eligibilityMemory struct {
	pages   [][]model.GameSummary
	rows    map[string][]model.EligibilityRow
	cursors []string
	limits  []int
	reads   []string
	tags    []string
	failure error
}

func (memory *eligibilityMemory) GamePage(_ context.Context, _ string, title, id string, limit int) ([]model.GameSummary, bool, error) {
	memory.cursors = append(memory.cursors, title+":"+id)
	memory.limits = append(memory.limits, limit)
	if memory.failure != nil {
		return nil, false, memory.failure
	}
	page := memory.pages[0]
	memory.pages = memory.pages[1:]
	return page, len(memory.pages) > 0, nil
}

func (memory *eligibilityMemory) Rows(_ context.Context, id string) ([]model.EligibilityRow, error) {
	memory.reads = append(memory.reads, id)
	return memory.rows[id], memory.failure
}

func (memory *eligibilityMemory) ArcadeDependencies(context.Context, string, string) ([]model.ArcadeDependencyRow, error) {
	return nil, memory.failure
}

func (memory *eligibilityMemory) DependencyFileCount(context.Context, string, string, string) (int, error) {
	return 0, memory.failure
}

func (memory *eligibilityMemory) References(_ context.Context, ids []string) (map[string][]tagging.Reference, error) {
	memory.tags = slices.Clone(ids)
	return nil, memory.failure
}

type eligibilityBIOS struct {
	calls   int
	status  string
	failure error
}

func (bios *eligibilityBIOS) ResolveBIOS(context.Context, string, string, string) (corevalidation.Snapshot, string, string, error) {
	bios.calls++
	return corevalidation.Snapshot{SchemaVersion: 1, Kind: "STATIC", BIOS: []corevalidation.BIOSDependency{}}, bios.status, "", bios.failure
}

func eligibilityRegistry() *profile.Registry {
	return &profile.Registry{Manifest: profile.Manifest{
		Protocol: profile.Protocol{AllowedContentKinds: []string{"SINGLE_FILE"}},
		Profiles: []profile.ManifestProfile{{ID: "registered", CoreID: "core", ProviderID: "provider", TargetID: "target", PlatformIDs: []string{"platform"}, MaxPlayers: 2}},
	}}
}

func readyEligibilityRow() model.EligibilityRow {
	return model.EligibilityRow{VariantID: "variant", CoreID: "core", ProviderID: "provider", TargetID: "target", PlatformID: "platform", ContentKind: "SINGLE_FILE", LogicalName: "game.rom", DependencyJSON: `{"schemaVersion":1,"kind":"STATIC","bios":[]}`}
}

func TestEligibilityRejectsInvalidPageBeforeReading(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		availability, title, id string
		limit                   int
	}{
		{availability: "unknown", limit: 1}, {limit: 0}, {limit: 101}, {title: "a", limit: 1}, {id: "a", limit: 1},
	} {
		memory := &eligibilityMemory{}
		service := NewEligibility(memory, nil, memory, nil)
		_, _, err := service.GamePage(t.Context(), "actor", test.availability, test.title, test.id, test.limit)
		if !errors.Is(err, model.ErrInvalidProfile) || len(memory.cursors) != 0 {
			t.Fatalf("invalid page: error=%v, reads=%v", err, memory.cursors)
		}
	}
}

func TestSupportedPageBoundsEligibilityAndTagsAfterFiltering(t *testing.T) {
	t.Parallel()
	memory := &eligibilityMemory{pages: [][]model.GameSummary{
		{{GameID: "hidden", Title: "A"}, {GameID: "one", Title: "B"}},
		{{GameID: "two", Title: "C"}, {GameID: "unused", Title: "D"}},
	}, rows: map[string][]model.EligibilityRow{"one": {readyEligibilityRow()}, "two": {readyEligibilityRow()}}}
	bios := &eligibilityBIOS{status: "READY"}
	service := NewEligibility(memory, eligibilityRegistry(), memory, bios)
	page, more, err := service.GamePage(t.Context(), "actor", "SUPPORTED", "", "", 1)
	if err != nil || !more || len(page) != 1 || page[0].GameID != "one" || page[0].Tags == nil {
		t.Fatalf("page=%+v more=%v error=%v", page, more, err)
	}
	if !slices.Equal(memory.reads, []string{"hidden", "one", "two"}) || !slices.Equal(memory.tags, []string{"one"}) || !slices.Equal(memory.cursors, []string{":", "b:one"}) || !slices.Equal(memory.limits, []int{2, 2}) {
		t.Fatalf("read=%v tags=%v cursors=%v limits=%v", memory.reads, memory.tags, memory.cursors, memory.limits)
	}
}

func TestEligibilityRequiresCurrentDependenciesAndExactTarget(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name            string
		change          func(*model.EligibilityRow)
		status, blocker string
		biosCalls       int
	}{
		{name: "ready", status: "READY", biosCalls: 1},
		{name: "dependency changed", status: "BLOCKED", blocker: "DEPENDENCY_STALE", biosCalls: 1},
		{name: "bad snapshot", status: "READY", change: func(row *model.EligibilityRow) { row.DependencyJSON = "broken" }, blocker: "DEPENDENCY_STALE"},
		{name: "wrong platform", change: func(row *model.EligibilityRow) { row.PlatformID = "other" }, blocker: "CORE_NOT_ALLOWLISTED"},
		{name: "wrong provider", change: func(row *model.EligibilityRow) { row.ProviderID = "other" }, blocker: "CORE_NOT_ALLOWLISTED"},
		{name: "wrong content", change: func(row *model.EligibilityRow) { row.ContentKind = "MULTI_FILE" }, blocker: "CONTENT_NOT_ALLOWLISTED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := readyEligibilityRow()
			if test.change != nil {
				test.change(&row)
			}
			memory := &eligibilityMemory{rows: map[string][]model.EligibilityRow{"game": {row}}}
			bios := &eligibilityBIOS{status: test.status}
			service := NewEligibility(memory, eligibilityRegistry(), memory, bios)
			result, blocker, err := service.profileEligibility(t.Context(), "game")
			if err != nil || bios.calls != test.biosCalls {
				t.Fatalf("error=%v BIOS calls=%d", err, bios.calls)
			}
			if test.blocker == "" {
				if len(result) != 1 {
					t.Fatalf("eligible=%+v", result)
				}
			} else if len(result) != 0 || blocker != test.blocker {
				t.Fatalf("eligible=%+v blocker=%s", result, blocker)
			}
		})
	}
}

func TestEligibilityPreservesRepositoryAndBIOSFailures(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("storage unavailable")
	memory := &eligibilityMemory{failure: sentinel}
	service := NewEligibility(memory, eligibilityRegistry(), memory, nil)
	if _, err := service.Profiles(t.Context(), "game"); !errors.Is(err, sentinel) {
		t.Fatalf("repository failure=%v", err)
	}
	memory.failure = nil
	memory.rows = map[string][]model.EligibilityRow{"game": {readyEligibilityRow()}}
	service.bios = &eligibilityBIOS{failure: sentinel}
	if _, err := service.Profiles(t.Context(), "game"); !errors.Is(err, sentinel) {
		t.Fatalf("BIOS failure=%v", err)
	}
}
