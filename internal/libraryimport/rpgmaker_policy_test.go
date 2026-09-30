package libraryimport

import (
	"testing"

	libraryservice "retrom/internal/service/libraryimport"

	"retrom/internal/core/rpgmaker/detector"
)

func TestRPGProjectResourcesPolicyPreservesExplicitConfirmation(t *testing.T) {
	for _, generation := range []string{"RPG2000", "RPG2003", "RPGXP", "RPGVX", "RPGVXACE"} {
		t.Run(generation, func(t *testing.T) {
			analysis := libraryservice.RPGReviewAnalysis{}
			analysis.Requirements.RTP = []detector.RTPDependency{{Slot: 1, DeclaredName: "Standard"}}
			blocked := libraryservice.ResolveRPGResourcePolicy(generation, false, analysis)
			if blocked.Status != "BLOCKED" || blocked.Code != "RPG_EXTERNAL_RTP_REQUIRED" {
				t.Fatalf("external dependency: %+v", blocked)
			}
			ready := libraryservice.ResolveRPGResourcePolicy(generation, true, analysis)
			if ready.Status != "READY" || ready.Digest == blocked.Digest {
				t.Fatalf("explicit confirmation: %+v", ready)
			}
			again := libraryservice.ResolveRPGResourcePolicy(generation, false, analysis)
			if again.Status != "BLOCKED" || again.Digest != blocked.Digest {
				t.Fatal("clearing confirmation did not restore dependency check")
			}
		})
	}
}

func TestSelfContainedRPGProjectsNeedNoPack(t *testing.T) {
	for _, generation := range []string{"RPG2000", "RPG2003", "RPGXP", "RPGVX", "RPGVXACE", "RPGMV", "RPGMZ"} {
		analysis := libraryservice.RPGReviewAnalysis{}
		analysis.SelfContained = true
		result := libraryservice.ResolveRPGResourcePolicy(generation, false, analysis)
		if result.Status != "READY" {
			t.Errorf("self-contained %s: %+v", generation, result)
		}
	}
}
