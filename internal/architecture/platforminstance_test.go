package architecture

import "testing"

func TestPlatformInstanceUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../service/platforminstance")
}
