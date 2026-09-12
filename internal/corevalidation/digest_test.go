package corevalidation

import (
	"strings"
	"testing"
)

func TestValidationDigestsPreserveOptionalDATEncoding(t *testing.T) {
	t.Parallel()
	datID := "dat-1"
	for _, test := range []struct {
		id              *string
		provider, multi string
	}{
		{nil, "7fc75f06698e542bb4c66e5362d58c1e6cf2ceec19686f8089c603aac363d0a1", "f2053b4db182929ae5863ef1f53be3ed38ceec46d4c798a75ad6bb1367f6257c"},
		{&datID, "c4a2dd8069a41d945e55aeccebdf791e747d094374a591ace190b65069b711fd", "ea564e46c1ce6313e777c673ee862cd95aa3f73c61f83bf25ca607771a72dd40"},
	} {
		snapshot := Snapshot{SchemaVersion: 1, Kind: SnapshotKindStatic, BIOS: []BIOSDependency{}}
		provider, err := ProviderValidationInputDigest("provider", "target", "game", test.id, snapshot)
		if err != nil || provider != test.provider {
			t.Fatalf("provider digest=%s error=%v", provider, err)
		}
		multi, err := MultiDiscValidationInputDigest(MultiDiscValidationInput{
			GameVariantID: "variant", GameID: "game", ContentKind: MultiDiscContentKind, ProviderID: "provider", TargetID: "target",
			ContentPolicySHA256: strings.Repeat("a", 64), DATVersionID: test.id, BIOSDependencySHA256: strings.Repeat("b", 64),
			OrderedDiscSHA256: []string{strings.Repeat("c", 64), strings.Repeat("d", 64)}, CanonicalPlaylistSHA256: strings.Repeat("e", 64),
		})
		if err != nil || multi != test.multi {
			t.Fatalf("multi-disc digest=%s error=%v", multi, err)
		}
	}
}
