package libraryimport

import (
	"encoding/json"
	"testing"

	"retrom/internal/contentcapability"
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
		contentPolicy:  contentcapability.NewPolicy("SINGLE_FILE"),
		dependencyJSON: `{"schemaVersion":1,"dependencies":[]}`, status: "READY", compatibilityCode: "READY",
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

func TestContentPolicyDigestSurvivesSnapshotJSONKeyOrder(t *testing.T) {
	t.Parallel()
	original := contentcapability.NewPolicy("SINGLE_FILE")
	var restored contentcapability.Policy
	if err := json.Unmarshal([]byte(`{"multiDisc":null,"supportedContentKinds":["SINGLE_FILE"]}`), &restored); err != nil {
		t.Fatal(err)
	}
	if original.Digest() != restored.Digest() {
		t.Fatal("snapshot round-trip changed the content policy digest")
	}
}

func TestContentPolicyDigestTreatsAcceptedKindsAsASet(t *testing.T) {
	t.Parallel()
	first := contentcapability.NewPolicy("MULTI_DISC", "SINGLE_FILE")
	second := contentcapability.NewPolicy("SINGLE_FILE", "MULTI_DISC")
	if first.Digest() != second.Digest() {
		t.Fatal("accepted-kind order changed the policy digest")
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
	single := contentcapability.NewPolicy("SINGLE_FILE")
	expanded := contentcapability.NewPolicy("SINGLE_FILE", "MULTI_DISC")
	if single.DigestFor("SINGLE_FILE") != expanded.DigestFor("SINGLE_FILE") {
		t.Fatal("unrelated content capability invalidated existing content")
	}
	changed := contentcapability.NewPolicy("MULTI_DISC")
	changed.MultiDisc.MaxDiscs = 4
	if expanded.DigestFor("MULTI_DISC") == changed.DigestFor("MULTI_DISC") {
		t.Fatal("selected content policy change was ignored")
	}
	if single.DigestFor("MULTI_DISC") != "" || (contentcapability.Policy{}).Digest() != "" {
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
