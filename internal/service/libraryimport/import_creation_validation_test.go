package libraryimport

import (
	"context"
	"testing"

	core "retrom/internal/core/validation"
	validation "retrom/internal/service/corevalidation"
)

type playlistBIOS struct{ validation.Repository }

func (playlistBIOS) BIOS(context.Context, string, string) ([]validation.BIOSRecord, error) {
	condition := "PCE_CD_CONTENT"
	return []validation.BIOSRecord{{Dependency: core.BIOSDependency{BIOSCatalogEntry: core.BIOSCatalogEntry{
		RequirementID: "cd-bios", LogicalName: "syscard3.pce", RequirementMode: "REQUIRED", ConditionCode: &condition,
	}}}}, nil
}

func TestMissingAllDiscsPreservesContentAndBIOSFacts(t *testing.T) {
	groups := []PreparedGroup{{
		ContentKind: "MULTI_DISC", ValidationStatus: "BLOCKED", CompatibilityCode: "MULTI_DISC_FILE_MISSING",
		Sources:      []PreparedSource{{Role: "PLAYLIST_SOURCE", LogicalName: "game.m3u"}},
		MultiEntries: []PreparedMultiDiscEntry{{SourceReference: "first.chd"}, {SourceReference: "second.chd"}},
		MultiDependency: &core.MultiDiscSnapshot{DiscCount: 2, MissingEntries: []core.MultiDiscMissingEntry{
			{Ordinal: 0, SourceReference: "first.chd", NormalizedReference: "first.chd"},
			{Ordinal: 1, SourceReference: "second.chd", NormalizedReference: "second.chd"},
		}},
	}}
	err := PrepareCreationStaticBIOS(t.Context(), playlistBIOS{}, ImportTarget{PlatformID: "pce", ProviderID: "provider", TargetID: "target"}, groups)
	if err != nil || groups[0].CompatibilityCode != "MULTI_DISC_FILE_MISSING" || groups[0].ValidationStatus != "BLOCKED" {
		t.Fatalf("group=%+v error=%v", groups[0], err)
	}
	snapshot, err := core.ParseSnapshot(groups[0].DependencySnapshot)
	if err != nil || len(snapshot.BIOS) != 1 || snapshot.MultiDisc == nil || len(snapshot.MultiDisc.MissingEntries) != 2 {
		t.Fatalf("snapshot=%+v error=%v", snapshot, err)
	}
}
