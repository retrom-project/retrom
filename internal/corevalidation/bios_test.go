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

func TestParseRuntimeBIOSDependenciesAcceptsStaticAndArcadeSnapshots(t *testing.T) {
	t.Parallel()
	static := `{"schemaVersion":1,"bios":[{"requirementId":"bios","requirementVersion":1,"catalogDigest":"digest","logicalName":"bios.bin","requirementMode":"REQUIRED","conditionCode":null,"deliveryKind":"EXTERNAL_FILE","emulatorPath":"/bios.bin","activationOptions":{},"installationId":"installation","installationVersion":1,"blobId":"blob","installationStatus":"MATCHED"}]}`
	dependencies, err := ParseRuntimeBIOSDependencies(static)
	if err != nil || len(dependencies) != 1 || dependencies[0].LogicalName != "bios.bin" {
		t.Fatalf("ParseRuntimeBIOSDependencies(static) = %#v, error=%v", dependencies, err)
	}

	arcade := `{"schemaVersion":2,"machine":"nbbatman","datVersionId":"dat-version","closure":[],"dependencies":[{"kind":"BIOS_OR_BASE","machine":"deco32","state":"SATISFIED_EXTERNAL","requiredEntries":["mb7124h.16r"]}],"missingEntries":[],"mismatchedEntries":[],"warnings":[]}`
	dependencies, err = ParseRuntimeBIOSDependencies(arcade)
	if err != nil || len(dependencies) != 0 {
		t.Fatalf("ParseRuntimeBIOSDependencies(arcade) = %#v, error=%v", dependencies, err)
	}
}

func TestParseRuntimeBIOSDependenciesRejectsMalformedSnapshots(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"schemaVersion":2,"machine":"nbbatman","datVersionId":"dat-version","closure":null,"dependencies":[],"missingEntries":[],"mismatchedEntries":[],"warnings":[]}`,
		`{"schemaVersion":2,"machine":"nbbatman","datVersionId":"dat-version","closure":[],"dependencies":[],"missingEntries":[],"mismatchedEntries":[],"warnings":[],"unknown":true}`,
		`{"schemaVersion":3,"bios":[]}`,
	} {
		if _, err := ParseRuntimeBIOSDependencies(raw); !errors.Is(err, ErrInvalidSnapshot) {
			t.Errorf("ParseRuntimeBIOSDependencies(%s) error = %v", raw, err)
		}
	}
}
