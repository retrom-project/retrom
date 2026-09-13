package libraryimport

import "testing"

func TestRPGCreationTargetGuardUsesStableProviderTargetIdentity(t *testing.T) {
	t.Parallel()
	target := creationTarget{
		CoreID: "rpgmaker", ProviderID: "retrom-runtime", TargetID: "rpgmaker-2000",
	}
	guard := targetGuard(target)
	if guard.ProviderID != target.ProviderID || guard.TargetID != target.TargetID || guard.CoreID != "rpgmaker" {
		t.Fatalf("provider target guard = %#v", guard)
	}
}
