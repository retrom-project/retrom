package libraryimport

import "testing"

func TestRPGCreationTargetGuardUsesStableProviderTargetIdentity(t *testing.T) {
	t.Parallel()
	target := creationTarget{
		coreID: "rpgmaker", providerID: "retrom-runtime", targetID: "rpgmaker-2000",
	}
	guard := targetGuard(target)
	if guard.ProviderID != target.providerID || guard.TargetID != target.targetID || guard.CoreID != "rpgmaker" {
		t.Fatalf("provider target guard = %#v", guard)
	}
}
