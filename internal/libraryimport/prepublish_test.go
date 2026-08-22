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
		SchemaVersion:             1,
		ValidatorVersion:          validatorReviewV4,
		SourceSnapshotID:          "snapshot-1",
		SourceManifestDigest:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ContentKind:               "SINGLE_FILE",
		TargetPlatformInstanceID:  "platform-instance-1",
		PlatformInstanceVersion:   7,
		CoreArtifactID:            "artifact-1",
		CoreArtifactVersion:       3,
		CompatibilityConfigDigest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		DATVersionID:              &datID,
		DependencySnapshot:        json.RawMessage(`{"schemaVersion":2,"dependencies":[]}`),
		Status:                    "READY",
		CompatibilityCode:         "READY",
	}
	const expected = "b1e236a2437a00b63db3cf3c5f9001b1180d249b8cbbdeeccdbc346153a230f4"
	if got := prepublishDigest(base); got != expected {
		t.Fatalf("prepublish golden = %s", got)
	}
	changedArtifact := base
	changedArtifact.CoreArtifactVersion++
	changedCompatibility := base
	changedCompatibility.CompatibilityConfigDigest = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	changedKind := base
	changedKind.ContentKind = "DOS_BUNDLE"
	testassert.False(t, testassert.Any(func() bool { return prepublishDigest(base) == prepublishDigest(changedArtifact) }, func() bool { return prepublishDigest(base) == prepublishDigest(changedCompatibility) }, func() bool { return prepublishDigest(base) == prepublishDigest(changedKind) }), "prepublish digest ignored a semantic validation input")
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
