package contentcapability

import (
	"slices"
	"testing"
)

func TestPolicyConstructionOwnsCanonicalKindsAndLimits(t *testing.T) {
	t.Parallel()
	kinds := []string{"SINGLE_FILE", ModeMultiDisc, "SINGLE_FILE"}
	policy := NewPolicy(kinds...)
	if !slices.Equal(kinds, []string{"SINGLE_FILE", ModeMultiDisc, "SINGLE_FILE"}) ||
		!slices.Equal(policy.SupportedContentKinds, []string{ModeMultiDisc, "SINGLE_FILE"}) {
		t.Fatal("construction mutated the caller or retained duplicate/order-dependent kinds")
	}
	kinds[0] = "CHANGED"
	if !policy.Supports("SINGLE_FILE") || policy.Supports("CHANGED") {
		t.Fatal("policy retained mutable caller storage")
	}
	policy.MultiDisc.MaxDiscs = 4
	capabilities := Resolve("saturn", true, true, policy)
	if capabilities.MultiDisc == nil || capabilities.MultiDisc.MaxDiscs != 4 {
		t.Fatal("import admission ignored the typed policy")
	}
	capabilities.MultiDisc.MaxDiscs = 2
	if policy.MultiDisc.MaxDiscs != 4 || NewPolicy(ModeMultiDisc).MultiDisc.MaxDiscs != MaximumMultiDiscCount {
		t.Fatal("policies or import capabilities share mutable limits")
	}
}

func TestPolicyDigestCanonicalizesRestoredKindOrderWithoutMutation(t *testing.T) {
	t.Parallel()
	policy := NewPolicy("SINGLE_FILE", ModeMultiDisc)
	policy.SupportedContentKinds = []string{"SINGLE_FILE", ModeMultiDisc, "SINGLE_FILE"}
	if policy.Digest() != NewPolicy(ModeMultiDisc, "SINGLE_FILE").Digest() ||
		!slices.Equal(policy.SupportedContentKinds, []string{"SINGLE_FILE", ModeMultiDisc, "SINGLE_FILE"}) {
		t.Fatal("hash boundary mutated or failed to canonicalize a restored policy")
	}
}
