package detector

import "testing"

func TestRGSSRTPDeclarationsIncludeUnnumberedAndNumberedKeys(t *testing.T) {
	for _, key := range []string{"rtp", "rtp1", "rtp2", "rtp3"} {
		declarations := rgssRTPDependencies(map[string]string{key: " Standard "})
		if len(declarations) != 1 || declarations[0].DeclaredName != "Standard" {
			t.Fatalf("%s declaration ignored: %+v", key, declarations)
		}
		for _, generation := range []Generation{RPGXP, RPGVX, RPGVXAce} {
			if len(ExternalRTPRequirements(generation, true, declarations)) != 1 {
				t.Fatalf("%s declaration bypassed: %s", generation, key)
			}
		}
	}
	if len(rgssRTPDependencies(map[string]string{"rtp": "  "})) != 0 {
		t.Fatal("empty RTP is not an external dependency")
	}
}
