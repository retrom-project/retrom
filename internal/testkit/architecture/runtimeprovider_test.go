package architecture

import "testing"

func TestRuntimeProviderActivationUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../../adapter/runtime/runtimeprovider")
	assertBusinessImports(t, "../../service/runtimeprovider")
}
