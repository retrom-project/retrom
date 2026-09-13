package firmware

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestBIOSWorkflowsUseBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
