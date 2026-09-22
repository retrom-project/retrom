package architecture

import "testing"

func TestSourceAdaptersDoNotConstructPersistence(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../sourceimport")
}
