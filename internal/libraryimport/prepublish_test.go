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
		TargetContractSHA256:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		GameCompatibilityLine:    "fbneo-v1",
		ContentPolicyDigest:      "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		DATVersionID:             &datID,
		DependencySnapshot:       json.RawMessage(`{"schemaVersion":2,"dependencies":[]}`),
		Status:                   "READY",
		CompatibilityCode:        "READY",
	}
	const expected = "5d407b095c7c858bd4e1a5a4087db6f873b1317420935af346b48e2275efe0aa"
	if got := prepublishDigest(base); got != expected {
		t.Fatalf("prepublish golden = %s", got)
	}
	changedTarget := base
	changedTarget.TargetContractSHA256 = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	changedCompatibility := base
	changedCompatibility.ContentPolicyDigest = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	changedKind := base
	changedKind.ContentKind = "DOS_BUNDLE"
	testassert.False(t, testassert.Any(func() bool { return prepublishDigest(base) == prepublishDigest(changedTarget) }, func() bool { return prepublishDigest(base) == prepublishDigest(changedCompatibility) }, func() bool { return prepublishDigest(base) == prepublishDigest(changedKind) }), "prepublish digest ignored a semantic validation input")
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
