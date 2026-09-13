package uploads

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestUploadWorkflowsUseBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
