package gamecontent

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestGameContentUsesDatabasePorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
