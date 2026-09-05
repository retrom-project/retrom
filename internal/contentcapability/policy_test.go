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

func TestPolicyScanClearsPreviousRowAndConstructsDerivedFacts(t *testing.T) {
	t.Parallel()
	var policy Policy
	for _, column := range []any{"SINGLE_FILE,MULTI_DISC", []byte("MULTI_DISC,SINGLE_FILE")} {
		if err := policy.Scan(column); err != nil {
			t.Fatal(err)
		}
		if policy.Digest() != NewPolicy("SINGLE_FILE", ModeMultiDisc).Digest() ||
			policy.MultiDisc.Delivery != DeliveryEagerExternal {
			t.Fatal("relational query and constructor produced different policies")
		}
	}
	for _, column := range []any{nil, "", []byte{}} {
		policy = NewPolicy(ModeMultiDisc)
		if err := policy.Scan(column); err != nil || policy.MultiDisc != nil || policy.Supports(ModeMultiDisc) {
			t.Fatalf("absent binding inherited the prior row: %+v, %v", policy, err)
		}
	}
	policy = NewPolicy(ModeMultiDisc)
	if err := policy.Scan(17); err == nil || policy.MultiDisc != nil || policy.Digest() != "" {
		t.Fatal("invalid SQL column retained or admitted capabilities")
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
