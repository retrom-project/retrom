package architecture

import "testing"

func TestUploadWorkflowsUseBusinessPorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../../service/uploads")
}
