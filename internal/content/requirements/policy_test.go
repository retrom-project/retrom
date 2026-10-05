package requirements

import (
	"testing"

	"retrom/internal/format/nintendo3ds"
)

func TestTargetRequiresParsedDecryptedExecutable(t *testing.T) {
	policy := Policy{Kind: Decrypted3DS}
	if got := policy.Evaluate(Facts{}, "old.cci"); got == nil || got.Code != "THREEDS_CONTAINER_INVALID" {
		t.Fatalf("missing facts accepted: %+v", got)
	}
	facts := Facts{Nintendo3DS: &nintendo3ds.Facts{Format: "NCSD", Partitions: []nintendo3ds.Partition{{Index: 0, Executable: true, Encrypted: true}, {Index: 1, Encrypted: false}}}}
	if got := policy.Evaluate(facts, "encrypted.cci"); got == nil || got.Code != "THREEDS_ENCRYPTED_CONTENT" {
		t.Fatalf("encrypted main accepted: %+v", got)
	}
	facts.Nintendo3DS.Partitions[0].Encrypted = false
	facts.Nintendo3DS.Partitions[1].Encrypted = true
	if got := policy.Evaluate(facts, "decrypted.cci"); got != nil {
		t.Fatalf("non-game partition changed executable admission: %+v", got)
	}
}
