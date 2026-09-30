package libraryimport

import (
	"encoding/json"
	"testing"

	contentcapability "retrom/internal/content/capability"
)

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
	if got := preparedGroupContentKind(preparedGroup{Sources: []preparedSource{{Role: "CONTENT"}}}); got != "SINGLE_FILE" {
		t.Fatalf("single content kind = %s", got)
	}
	if got := preparedGroupContentKind(preparedGroup{Sources: []preparedSource{{Role: "DOS_SOURCE"}}}); got != "DOS_BUNDLE" {
		t.Fatalf("DOS content kind = %s", got)
	}
}
