package corevalidation

import (
	"errors"
	"testing"
)

func TestBIOSAppliesUsesOnlyCanonicalContentSuffix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		condition, name string
		want            bool
	}{
		{"FDS_CONTENT", "Disk.FDS", true},
		{"FDS_CONTENT", "Disk.fds.zip", false},
		{"GB_CONTENT", "Game.dmg", true},
		{"GBC_CONTENT", "Game.GBC", true},
		{"GBA_CONTENT", "Game.gba", true},
		{"GAME_GENIE_ADDON_MODE", "Game.nes", false},
		{"", "Game.chd", true},
	}
	for _, test := range tests {
		if actual := BIOSApplies(test.condition, test.name); actual != test.want {
			t.Errorf("BIOSApplies(%q, %q) = %t", test.condition, test.name, actual)
		}
	}
}

func TestMultiDiscSnapshotRoundTripsAndRejectsInvalidEvidence(t *testing.T) {
	t.Parallel()
	snapshot := Snapshot{
		SchemaVersion: SnapshotSchemaVersion,
		BIOS:          []BIOSDependency{},
		MultiDisc: &MultiDiscSnapshot{
			DiscCount: 3,
			MissingEntries: []MultiDiscMissingEntry{{
				Ordinal: 2, SourceReference: "Disc 3.chd", NormalizedReference: "disc 3.chd",
			}},
		},
	}
	encoded, err := snapshot.JSON()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseSnapshot(string(encoded))
	if err != nil || parsed.MultiDisc == nil || parsed.MultiDisc.DiscCount != 3 ||
		len(parsed.MultiDisc.MissingEntries) != 1 {
		t.Fatalf("ParseSnapshot() = %#v, error=%v", parsed, err)
	}
	for _, raw := range []string{
		`{"schemaVersion":1,"bios":[],"multiDisc":{"discCount":1,"missingEntries":[]}}`,
		`{"schemaVersion":1,"bios":[],"multiDisc":{"discCount":2,"missingEntries":[{"ordinal":2,"sourceReference":"x","normalizedReference":"x"}]}}`,
		`{"schemaVersion":1,"bios":[],"multiDisc":{"discCount":2,"missingEntries":null}}`,
	} {
		if _, err := ParseSnapshot(raw); !errors.Is(err, ErrInvalidSnapshot) {
			t.Errorf("ParseSnapshot(%s) error = %v", raw, err)
		}
	}
}
