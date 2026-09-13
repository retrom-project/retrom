package saves

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestCheckpointWorkflowsUseBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
