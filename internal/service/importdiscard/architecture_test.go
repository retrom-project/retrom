package importdiscard

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestImportDiscardUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
