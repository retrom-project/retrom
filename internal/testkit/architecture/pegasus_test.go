package architecture

import "testing"

func TestPegasusAdaptersDoNotConstructPersistence(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../../adapter/imports/pegasusimport")
}
