package contentcapability

import "testing"

func TestFileLimitsAndDigestFollowDeliveredRole(t *testing.T) {
	policy := NewPolicy("SINGLE_FILE", ModeMultiDisc)
	policy.InputMaxFileBytes = map[string]int64{"game": 10, "discs": 20}
	if policy.CheckFile("SINGLE_FILE", "file", 10) != nil || policy.CheckFile(ModeMultiDisc, "disc", 20) != nil {
		t.Fatal("inclusive limits rejected")
	}
	rejection := policy.CheckFile("SINGLE_FILE", "file", 11)
	if rejection == nil || rejection.Limit.Actual != 11 || rejection.Limit.Maximum != 10 {
		t.Fatalf("rejection=%+v", rejection)
	}
	before := policy.DigestFor("SINGLE_FILE")
	policy.InputMaxFileBytes["discs"]++
	if policy.DigestFor("SINGLE_FILE") != before {
		t.Fatal("unrelated disc policy changed single-file identity")
	}
	policy.InputMaxFileBytes["game"]++
	if policy.DigestFor("SINGLE_FILE") == before {
		t.Fatal("limit change did not invalidate validation identity")
	}
}
