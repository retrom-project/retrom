package architecture

import "testing"

func TestRuntimeProviderActivationUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../runtimeprovider")
	assertBusinessImports(t, "../service/runtimeprovider")
}
