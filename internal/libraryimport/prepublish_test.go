package libraryimport

import (
	"encoding/json"
	"testing"

	"retrom/internal/testassert"
)

func TestReviewValidityIgnoresPlatformPresentationChangesButNotActualInputs(t *testing.T) {
	t.Parallel()
	evidence := reviewValidationEvidence{
		platformVersion: 1, currentPlatformVersion: 2,
		sourceSnapshotID: "source", draftSnapshotID: "source",
		platformInstanceID: "folder", draftPlatformInstanceID: "folder",
		coreID: "gambatte", currentCoreID: "gambatte", providerID: "emulatorjs", targetID: "gambatte",
		manifestDigest: "content", snapshotManifestDigest: "content", contentKind: "SINGLE_FILE",
		contentPolicyJSON: `{"schemaVersion":1,"supportedContentKinds":["SINGLE_FILE"],"multiDisc":null}`,
		dependencyJSON:    `{"schemaVersion":1,"dependencies":[]}`, status: "READY", compatibilityCode: "READY",
	}
	if _, current := evidence.currentInput(); !current {
		t.Fatal("a folder presentation edit invalidated validation")
	}
	for _, change := range []func(*reviewValidationEvidence){
		func(value *reviewValidationEvidence) { value.currentCoreID = "other-core" },
		func(value *reviewValidationEvidence) { value.draftSnapshotID = "replacement-source" },
		func(value *reviewValidationEvidence) { value.snapshotManifestDigest = "replacement-bytes" },
	} {
		changed := evidence
		change(&changed)
		if _, current := changed.currentInput(); current {
			t.Fatal("a real input change was ignored")
		}
	}
}

func TestPrepublishDigestGoldenAndSemanticInputs(t *testing.T) {
	t.Parallel()
	datID := "dat-1"
	base := prepublishDigestInput{
		SchemaVersion:            1,
		SourceSnapshotID:         "snapshot-1",
		SourceManifestDigest:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ContentKind:              "SINGLE_FILE",
		TargetPlatformInstanceID: "platform-instance-1",
		ProviderID:               "emulatorjs",
		TargetID:                 "fbneo",
		ContentPolicyDigest:      "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		DATVersionID:             &datID,
		DependencySnapshot:       json.RawMessage(`{"schemaVersion":1,"kind":"ARCADE","machine":"pacman","datVersionId":"dat-1","closure":[],"dependencies":[],"missingEntries":[],"mismatchedEntries":[],"warnings":[]}`),
		Status:                   "READY",
		CompatibilityCode:        "READY",
	}
	const expected = "8fa29e4d0a2967907893757e0937abfdf665671f880a4de252e57f773dc40529"
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

func TestPrepublishDigestOnlyMatchesCurrentInputs(t *testing.T) {
	t.Parallel()
	current := prepublishDigestInput{
		SchemaVersion: 1, SourceSnapshotID: "snapshot", ContentKind: "SINGLE_FILE",
		DependencySnapshot: json.RawMessage(`{"schemaVersion":1,"dependencies":[]}`),
	}
	digest := prepublishDigest(current)
	if !prepublishDigestMatches(digest, current) {
		t.Fatal("current input did not match")
	}
	changed := current
	changed.SourceSnapshotID = "other-source"
	for _, candidate := range []string{"", "not-a-digest", prepublishDigest(changed)} {
		if prepublishDigestMatches(candidate, current) {
			t.Fatal("invalid or different input matched")
		}
	}
	current.DependencySnapshot = json.RawMessage("{")
	if prepublishDigest(current) != "" {
		t.Fatal("invalid input was hashed")
	}
}

func TestValidationPolicyDigestIgnoresUnrelatedCapabilities(t *testing.T) {
	t.Parallel()
	single := `{"schemaVersion":1,"supportedContentKinds":["SINGLE_FILE"]}`
	expanded := `{"schemaVersion":1,"supportedContentKinds":["SINGLE_FILE","MULTI_DISC"],"multiDisc":{"maxDiscs":8}}`
	if validationPolicyDigest(single, "SINGLE_FILE") != validationPolicyDigest(expanded, "SINGLE_FILE") {
		t.Fatal("unrelated content capability invalidated existing content")
	}
	changed := `{"schemaVersion":1,"supportedContentKinds":["MULTI_DISC"],"multiDisc":{"maxDiscs":4}}`
	if validationPolicyDigest(expanded, "MULTI_DISC") == validationPolicyDigest(changed, "MULTI_DISC") {
		t.Fatal("selected content policy change was ignored")
	}
	if validationPolicyDigest(single, "MULTI_DISC") != "" || compatibilityConfigDigest("{") != "" {
		t.Fatal("unsupported or invalid policy was accepted")
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
