package architecture

import "testing"

func TestCheckpointWorkflowsUseBusinessPorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../service/saves")
}
