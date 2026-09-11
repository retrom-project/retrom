package libraryimport

import (
	"testing"

	"retrom/internal/rpgmaker/detector"
)

func TestRPGProjectResourcesPolicyPreservesExplicitConfirmation(t *testing.T) {
	for _, generation := range []string{"RPG2000", "RPG2003", "RPGXP", "RPGVX", "RPGVXACE"} {
		t.Run(generation, func(t *testing.T) {
			profile := rpgReviewBinding{generation: generation}
			profile.analysis.Requirements.RTP = []detector.RTPDependency{{Slot: 1, DeclaredName: "Standard"}}
			blocked, blockedSHA := resolveRPGDependencies(profile)
			if blocked.status != "BLOCKED" || blocked.code != "RPG_EXTERNAL_RTP_REQUIRED" {
				t.Fatalf("external dependency: %+v", blocked)
			}
			profile.override = true
			ready, readySHA := resolveRPGDependencies(profile)
			if ready.status != "READY" || readySHA == blockedSHA {
				t.Fatalf("explicit confirmation: %+v", ready)
			}
			profile.override = false
			again, againSHA := resolveRPGDependencies(profile)
			if again.status != "BLOCKED" || againSHA != blockedSHA {
				t.Fatal("clearing confirmation did not restore dependency check")
			}
		})
	}
}

func TestSelfContainedRPGProjectsNeedNoPack(t *testing.T) {
	for _, generation := range []string{"RPG2000", "RPG2003", "RPGXP", "RPGVX", "RPGVXACE", "RPGMV", "RPGMZ"} {
		profile := rpgReviewBinding{generation: generation}
		profile.analysis.SelfContained = true
		result, _ := resolveRPGDependencies(profile)
		if result.status != "READY" {
			t.Errorf("self-contained %s: %+v", generation, result)
		}
	}
}
