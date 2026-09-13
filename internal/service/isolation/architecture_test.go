package isolation

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestIsolationDependsOnBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
