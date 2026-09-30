package arcade

import (
	"context"
	"testing"

	"retrom/internal/importing"
)

type currentArcadeCatalog struct{}

func (currentArcadeCatalog) MachineClassification(context.Context, string, string) (string, bool, error) {
	return "NORMAL", true, nil
}

func (currentArcadeCatalog) MachineRelation(_ context.Context, _, machine string) (MachineRelation, bool, error) {
	if machine == "child" {
		return MachineRelation{CloneOf: "parent"}, true, nil
	}
	return MachineRelation{}, machine == "parent", nil
}

func (currentArcadeCatalog) ArcadeRequirements(_ context.Context, _, machine string) (CatalogRequirements, error) {
	return CatalogRequirements{ROMs: []ROMRequirement{{Name: machine + ".bin", Size: 1, Status: "GOOD"}}}, nil
}

func TestCurrentArcadeReviewRejectsMergedPrimaryAndAllowsParentExtras(t *testing.T) {
	for _, test := range []struct {
		name, nestedArchive, status, code string
	}{
		{"merged primary", "CONTENT", "BLOCKED", "UNSUPPORTED_MERGED_ROMSET"},
		{"safe parent extra", "COMPANION", "READY", "READY"},
	} {
		t.Run(test.name, func(t *testing.T) {
			archives := []Archive{
				{Role: "CONTENT", LogicalName: "child.zip", Entries: map[string]importing.ArchiveEntry{
					"child.bin": {NormalizedPath: "child.bin", Size: 1},
				}},
				{Role: "COMPANION", LogicalName: "parent.zip", Entries: map[string]importing.ArchiveEntry{
					"parent.bin": {NormalizedPath: "parent.bin", Size: 1},
				}},
			}
			for _, archive := range archives {
				if archive.Role == test.nestedArchive {
					archive.Entries["nested/parent.bin"] = importing.ArchiveEntry{NormalizedPath: "nested/parent.bin", Size: 1}
				}
			}
			result, err := Resolve(t.Context(), currentArcadeCatalog{}, NoInstalledBIOS{}, "provider", "target", "dat", "child", archives)
			if err != nil || result.Status != test.status || result.Code != test.code {
				t.Fatalf("current review=%+v error=%v; want %s/%s", result, err, test.status, test.code)
			}
		})
	}
}
