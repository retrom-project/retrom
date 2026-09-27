package launch

import (
	"testing"

	"retrom/internal/testsupport/archcheck"
)

func TestProductionLaunchHasOneProviderTargetEnvelopePath(t *testing.T) {
	t.Parallel()
	archcheck.AssertNoSourceTokens(t, []string{
		"runtime_family",
		"runtimeFamily",
		"route_key",
		"routeKey",
		"core_artifacts",
		"core_artifact_id",
		"CoreArtifactID",
		"adapterAbi",
		"saveAbi",
		"payloadKind",
		"nativeProfile",
		"resumeSlot",
	})
	if count := archcheck.CountSourceToken(t, "runtimeBuilder.Build("); count != 1 {
		t.Fatalf("runtimeBuilder.Build calls = %d, want 1", count)
	}
}
