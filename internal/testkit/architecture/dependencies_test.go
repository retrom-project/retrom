package architecture

import "testing"

func TestDependencyRulesUseBusinessPorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../../adapter/runtime/dependencies")
	assertBusinessImports(t, "../../service/dependencies")
}
