package architecture

import "testing"

func TestGameContentUsesDatabasePorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../../service/gamecontent")
}
