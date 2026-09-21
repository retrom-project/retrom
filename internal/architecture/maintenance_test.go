package architecture

import "testing"

func TestMaintenanceUsesDatabasePorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../service/maintenance")
}
