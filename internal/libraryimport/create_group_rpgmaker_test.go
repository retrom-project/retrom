package libraryimport

import "testing"

func TestRPGCreationTargetGuardUsesOnlyProviderTargetContract(t *testing.T) {
	t.Parallel()
	target := creationTarget{
		coreID: "rpgmaker", providerID: "retrom-runtime", targetID: "rpgmaker-2000",
		targetContractSHA256:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		gameCompatibilityLine: "rpgmaker-2000-v1",
	}
	guard := targetGuard(target)
	if guard.ProviderID != target.providerID || guard.TargetID != target.targetID ||
		guard.TargetContractSHA256 != target.targetContractSHA256 ||
		guard.GameCompatibilityLine != target.gameCompatibilityLine || guard.CoreID != "rpgmaker" {
		t.Fatalf("provider target guard = %#v", guard)
	}
}
