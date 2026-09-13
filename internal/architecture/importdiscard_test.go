package architecture

import "testing"

func TestImportDiscardUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../service/importdiscard")
}
