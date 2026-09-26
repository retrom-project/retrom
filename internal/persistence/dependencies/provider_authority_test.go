package dependencies

import (
	"testing"

	"retrom/internal/testsupport/archcheck"
)

func TestDependencyProductionCodeDoesNotOwnRuntimeProviderContract(t *testing.T) {
	t.Parallel()
	archcheck.AssertNoSourceTokens(t, []string{
		"selected_core_artifacts",
		"adapter_abi",
		"runtime_family",
		"route_key",
		"RetromRuntimeFile",
		"RPGMakerVersion",
	})
}
