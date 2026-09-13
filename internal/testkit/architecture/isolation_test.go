package architecture

import "testing"

func TestIsolationDependsOnBusinessPorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../../service/isolation")
}
