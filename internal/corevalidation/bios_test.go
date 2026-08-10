package corevalidation

import (
	"database/sql"
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

func TestMultiDiscValidationInputDigestIsOrderedAndIncludesSemanticInputs(t *testing.T) {
	t.Parallel()
	input := MultiDiscValidationInput{
		GameVariantID: "variant", GameContentRevisionID: "content",
		ContentKind: MultiDiscContentKind, CoreArtifactID: "artifact", CoreArtifactVersion: 3,
		CompatibilityConfigSHA256: strings64("a"), DATVersionID: sql.NullString{},
		BIOSDependencySHA256:    strings64("b"),
		OrderedDiscSHA256:       []string{strings64("c"), strings64("d")},
		CanonicalPlaylistSHA256: strings64("e"),
	}
	first, err := MultiDiscValidationInputDigest(input)
	if err != nil || len(first) != 64 {
		t.Fatalf("MultiDiscValidationInputDigest() = %q, %v", first, err)
	}
	input.OrderedDiscSHA256[0], input.OrderedDiscSHA256[1] = input.OrderedDiscSHA256[1], input.OrderedDiscSHA256[0]
	second, err := MultiDiscValidationInputDigest(input)
	if err != nil || first == second {
		t.Fatalf("ordered digest did not change: first=%q second=%q err=%v", first, second, err)
	}
}

func strings64(value string) string {
	result := ""
	for range 64 {
		result += value
	}
	return result
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
