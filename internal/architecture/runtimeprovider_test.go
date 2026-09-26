package architecture

import "testing"

func TestRuntimeProviderActivationUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../runtime/provider")
	assertBusinessImports(t, "../service/runtimeprovider")
}
