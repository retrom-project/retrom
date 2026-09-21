package architecture

import "testing"

func TestNetplayTransportDoesNotConstructPersistence(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../netplay")
}
