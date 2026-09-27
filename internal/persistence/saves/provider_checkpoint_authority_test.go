package saves

import (
	"testing"

	"retrom/internal/testsupport/archcheck"
)

func TestSaveProductionCodeTreatsProviderCheckpointsAsOpaque(t *testing.T) {
	t.Parallel()
	archcheck.AssertNoSourceTokens(t, []string{
		"core_artifact",
		"runtime_family",
		"route_key",
		"adapter_abi",
		"save_abi",
		"payload_kind",
		"native_profile",
		"resume_slot",
		"rpgmaker/checkpoint",
	})
}
