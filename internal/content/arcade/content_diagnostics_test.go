package arcade

import (
	"context"
	"testing"

	"retrom/internal/importing"
)

type contentDiagnosticCatalog struct{ currentArcadeCatalog }

func (contentDiagnosticCatalog) ArcadeRequirements(context.Context, string, string) (CatalogRequirements, error) {
	return CatalogRequirements{ROMs: []ROMRequirement{
		{Name: "one.bin", Size: 1, Status: "GOOD"},
		{Name: "two.bin", Size: 2, Status: "GOOD"},
	}}, nil
}

func TestContentDiagnosticsDistinguishMissingAndWrongBytes(t *testing.T) {
	for _, test := range []struct {
		name, code        string
		entries           map[string]importing.ArchiveEntry
		missing, mismatch int
	}{
		{"missing", "ARCADE_CONTENT_MISSING_ENTRY", map[string]importing.ArchiveEntry{"one.bin": {NormalizedPath: "one.bin", Size: 1}}, 1, 0},
		{"wrong bytes", "ARCADE_CONTENT_MISMATCH", map[string]importing.ArchiveEntry{"one.bin": {NormalizedPath: "one.bin", Size: 10}, "two.bin": {NormalizedPath: "two.bin", Size: 2}}, 0, 1},
		{"both", "ARCADE_CONTENT_MISSING_AND_MISMATCHED", map[string]importing.ArchiveEntry{"one.bin": {NormalizedPath: "one.bin", Size: 10}}, 1, 1},
		{"matching", "READY", map[string]importing.ArchiveEntry{"one.bin": {NormalizedPath: "one.bin", Size: 1}, "two.bin": {NormalizedPath: "two.bin", Size: 2}}, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := Resolve(t.Context(), contentDiagnosticCatalog{}, NoInstalledBIOS{}, "provider", "target", "dat", "parent", []Archive{{Role: "CONTENT", LogicalName: "parent.zip", Entries: test.entries}})
			if err != nil || result.Code != test.code || len(result.Snapshot.MissingEntries) != test.missing || len(result.Snapshot.MismatchedEntries) != test.mismatch {
				t.Fatalf("result=%+v error=%v; want %s, missing=%d mismatch=%d", result, err, test.code, test.missing, test.mismatch)
			}
		})
	}
}
