package architecture

import "testing"

func TestBIOSWorkflowsUseBusinessPorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../service/firmware")
	assertBusinessImports(t, "../firmware")
}
