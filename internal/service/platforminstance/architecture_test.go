package platforminstance

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestPlatformInstanceUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
