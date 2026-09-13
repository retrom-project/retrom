package dependencies

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestDependencyAdapterUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
