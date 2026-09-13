package immersive

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestImmersiveUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
