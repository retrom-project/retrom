package maintenance

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestMaintenanceUsesDatabasePorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
