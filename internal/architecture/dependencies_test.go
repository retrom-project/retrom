package architecture

import "testing"

func TestDependencyRulesUseBusinessPorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../dependencies")
	assertBusinessImports(t, "../service/dependencies")
}
