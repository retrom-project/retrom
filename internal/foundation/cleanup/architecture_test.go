package cleanup

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestCleanupUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
