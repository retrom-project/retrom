package contentquery

import (
	"testing"

	"retrom/internal/contentcapability"
)

func TestPolicyScanClearsPreviousRowAndConstructsDerivedFacts(t *testing.T) {
	t.Parallel()
	var policy contentcapability.Policy
	for _, column := range []any{"SINGLE_FILE,MULTI_DISC", []byte("MULTI_DISC,SINGLE_FILE")} {
		if err := ScanPolicy(&policy).Scan(column); err != nil {
			t.Fatal(err)
		}
		if policy.Digest() != contentcapability.NewPolicy("SINGLE_FILE", contentcapability.ModeMultiDisc).Digest() ||
			policy.MultiDisc.Delivery != contentcapability.DeliveryEagerExternal {
			t.Fatal("relational query and constructor produced different policies")
		}
	}
	for _, column := range []any{nil, "", []byte{}} {
		policy = contentcapability.NewPolicy(contentcapability.ModeMultiDisc)
		if err := ScanPolicy(&policy).Scan(column); err != nil || policy.MultiDisc != nil || policy.Supports(contentcapability.ModeMultiDisc) {
			t.Fatalf("absent binding inherited the prior row: %+v, %v", policy, err)
		}
	}
	policy = contentcapability.NewPolicy(contentcapability.ModeMultiDisc)
	if err := ScanPolicy(&policy).Scan(17); err == nil || policy.MultiDisc != nil || policy.Digest() != "" {
		t.Fatal("invalid SQL column retained or admitted capabilities")
	}
}
