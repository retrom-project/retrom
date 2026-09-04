package libraryimport

import (
	"encoding/json"
	"testing"

	"retrom/internal/testassert"
)

func TestPrepublishDigestV4GoldenAndSemanticInputs(t *testing.T) {
	t.Parallel()
	datID := "dat-1"
	base := prepublishDigestInput{
		SchemaVersion:            1,
		ValidatorVersion:         validatorReviewV4,
		SourceSnapshotID:         "snapshot-1",
		SourceManifestDigest:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ContentKind:              "SINGLE_FILE",
		TargetPlatformInstanceID: "platform-instance-1",
		PlatformInstanceVersion:  7,
		ProviderID:               "emulatorjs",
		TargetID:                 "fbneo",
		ContentPolicyDigest:      "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		DATVersionID:             &datID,
		DependencySnapshot:       json.RawMessage(`{"schemaVersion":2,"dependencies":[]}`),
		Status:                   "READY",
		CompatibilityCode:        "READY",
	}
	const expected = "3d601c0757f378bedbe3ea4b6fde04a5bb6a6138c2af15a1da2f7ea83e925227"
	if got := prepublishDigest(base); got != expected {
		t.Fatalf("prepublish golden = %s", got)
	}
	changedTarget := base
	changedTarget.TargetID = "mednafen-saturn"
	changedCompatibility := base
	changedCompatibility.ContentPolicyDigest = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	changedKind := base
	changedKind.ContentKind = "DOS_BUNDLE"
	testassert.False(t, testassert.Any(func() bool { return prepublishDigest(base) == prepublishDigest(changedTarget) }, func() bool { return prepublishDigest(base) == prepublishDigest(changedCompatibility) }, func() bool { return prepublishDigest(base) == prepublishDigest(changedKind) }), "prepublish digest ignored a semantic validation input")
}

func TestCompatibilityConfigDigestIgnoresJSONObjectKeyOrder(t *testing.T) {
	t.Parallel()
	importOrder := `{"schemaVersion":1,"multiDisc":null,"supportedContentKinds":["SINGLE_FILE"]}`
	reviewOrder := `{"supportedContentKinds":["SINGLE_FILE"],"schemaVersion":1,"multiDisc":null}`
	if compatibilityConfigDigest(importOrder) != compatibilityConfigDigest(reviewOrder) {
		t.Fatal("semantically identical content policies produced different digests")
	}
}

func TestPrepublishDigestMatchesLegacyRawContentPolicy(t *testing.T) {
	t.Parallel()
	legacyPolicy := `{"schemaVersion":1,"supportedContentKinds":["SINGLE_FILE"],"multiDisc":null}`
	current := prepublishDigestInput{
		SchemaVersion: 1, ValidatorVersion: validatorReviewV4,
		SourceSnapshotID: "snapshot-1", SourceManifestDigest: "a",
		ContentKind: "SINGLE_FILE", TargetPlatformInstanceID: "platform-1",
		PlatformInstanceVersion: 1, ProviderID: "onscripter", TargetID: "onscripter-yuri",
		ContentPolicyDigest: compatibilityConfigDigest(legacyPolicy),
		DependencySnapshot:  json.RawMessage(`{"schemaVersion":2,"dependencies":[]}`),
		Status:              "BLOCKED", CompatibilityCode: "ONS_RUNTIME_TRIAL_REQUIRED",
	}
	legacy := current
	legacy.ContentPolicyDigest = legacyCompatibilityConfigDigest(legacyPolicy)
	if !prepublishDigestMatches(prepublishDigest(legacy), current, legacyPolicy) {
		t.Fatal("legacy validation using the raw content-policy digest became stale")
	}
}

func TestPreparedGroupContentKind(t *testing.T) {
	t.Parallel()
	if got := preparedGroupContentKind(preparedGroup{sources: []preparedSource{{role: "CONTENT"}}}); got != "SINGLE_FILE" {
		t.Fatalf("single content kind = %s", got)
	}
	if got := preparedGroupContentKind(preparedGroup{sources: []preparedSource{{role: "DOS_SOURCE"}}}); got != "DOS_BUNDLE" {
		t.Fatalf("DOS content kind = %s", got)
	}
}
