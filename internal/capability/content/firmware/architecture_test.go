package firmware

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestFirmwareCapabilityUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
